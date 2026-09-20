//go:build windows

package ops

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

// 极简注册表访问（advapi32）：生产路径**只读**，写入端仅测试脚手架使用。
//
// 为什么不引 golang.org/x/sys/windows/registry：
// 本项目刻意保持**零第三方依赖**（见 docs/02 决策：跨平台自研 trash、
// 不依赖 gio 等外部工具）。x/sys 虽属官方扩展库，但为读一个 DWORD 引入
// 整个模块并不划算，且会进入 go.mod 影响离线构建。这里直接用 syscall。

const (
	hkeyCurrentUser = 0x80000001
	keyRead         = 0x20019 // KEY_READ（含 KEY_QUERY_VALUE | KEY_ENUMERATE_SUB_KEYS）
	keyWrite        = 0x20006 // KEY_SET_VALUE | KEY_CREATE_SUB_KEY（仅测试脚手架）

	// regDWORD / regSZ 是 RegQueryValueExW 回填的值类型（winreg.h）。
	// ★ 2026-09-20（ocr M6）：registryGetDWORD 必须校验类型。HKCU 用户可写，
	// 同名 REG_SZ（如 "1"，恰好容得下 4 字节缓冲区）此前会被原样读出，
	// 把字符串首字节当成 DWORD——NukeOnDelete 判定随之出错。
	regDWORD = 4
	regSZ    = 1

	// regErrNotFound = ERROR_FILE_NOT_FOUND：RegOpenKeyExW（键不存在）与
	// RegQueryValueExW（值不存在）都返回它。★ 这是「用户没设过」这一**正常
	// 情形**与「读不到」（ACL 拒绝、类型不符）的唯一分界，必须与后者区分：
	// 前者可判 nukeOff，后者只能判 nukeUnknown。
	regErrNotFound = syscall.Errno(2)

	// RegCreateKeyExW 的 DISPOSITION 取值（winreg.h）。
	regValueNewlyCreated = 1
	regValueExists       = 2
)

// errRegTypeMismatch 值类型不是 REG_DWORD（registryGetDWORD 专用）。
var errRegTypeMismatch = errors.New("注册表值类型不是 REG_DWORD")

// regIsNotFound 判定注册表错误是否为「键/值不存在」。
// 注：ERROR_PATH_NOT_FOUND(3) 在父键不存在时出现，同样属于"没有这个设置"。
func regIsNotFound(err error) bool {
	return errors.Is(err, regErrNotFound) || errors.Is(err, syscall.Errno(3))
}

// regIsMismatch 判定 registryGetDWORD 是否因值类型不符而失败。
func regIsMismatch(err error) bool {
	return errors.Is(err, errRegTypeMismatch)
}

var (
	advapi32           = syscall.NewLazyDLL("advapi32.dll")
	procRegOpenKeyEx   = advapi32.NewProc("RegOpenKeyExW")
	procRegQueryValue  = advapi32.NewProc("RegQueryValueExW")
	procRegCloseKey    = advapi32.NewProc("RegCloseKey")
	procRegCreateKeyEx = advapi32.NewProc("RegCreateKeyExW")
	procRegSetValueEx  = advapi32.NewProc("RegSetValueExW")
	procRegDeleteKey   = advapi32.NewProc("RegDeleteTreeW")
)

// registryOpenKey 以只读方式打开 HKEY_CURRENT_USER 下的子键。
func registryOpenKey(sub string) (syscall.Handle, error) {
	p, err := syscall.UTF16PtrFromString(sub)
	if err != nil {
		return 0, err
	}
	var h syscall.Handle
	r0, _, _ := procRegOpenKeyEx.Call(
		uintptr(hkeyCurrentUser),
		uintptr(unsafe.Pointer(p)),
		0,
		keyRead,
		uintptr(unsafe.Pointer(&h)),
	)
	if r0 != 0 { // ERROR_SUCCESS == 0
		return 0, syscall.Errno(r0)
	}
	return h, nil
}

func registryCloseKey(h syscall.Handle) {
	if h != 0 {
		procRegCloseKey.Call(uintptr(h))
	}
}

// registryGetDWORD 读取一个 REG_DWORD 值。
//
// ★ 值类型必须核对（ocr M6）：RegQueryValueExW 只要**缓冲区装得下**就会
// 成功返回，类型完全不管。同名 REG_SZ "1" 会读出 0x00000031 → NukeOnDelete
// 被误判成"已禁用"；"0" 同理误判"未禁用"（后者更危险）。类型不符一律
// 返回错误，调用方按 nukeUnknown 处理——而不是猜一个值。
func registryGetDWORD(h syscall.Handle, name string) (uint32, error) {
	p, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	var (
		typ  uint32
		data uint32
		size = uint32(4)
	)
	r0, _, _ := procRegQueryValue.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(p)),
		0,
		uintptr(unsafe.Pointer(&typ)),
		uintptr(unsafe.Pointer(&data)),
		uintptr(unsafe.Pointer(&size)),
	)
	if r0 != 0 {
		return 0, syscall.Errno(r0)
	}
	if typ != regDWORD {
		return 0, fmt.Errorf("%w: 期望 REG_DWORD(4)，实际类型 %d", errRegTypeMismatch, typ)
	}
	return data, nil
}

// ---- 以下为**测试专用**的最小写入面（生产路径只读，勿在业务代码调用）----

// registryCreateKey 创建或打开 HKEY_CURRENT_USER 下的子键（写权限），
// 返回句柄与 DISPOSITION。
func registryCreateKey(sub string) (syscall.Handle, uint32, error) {
	p, err := syscall.UTF16PtrFromString(sub)
	if err != nil {
		return 0, 0, err
	}
	var (
		h    syscall.Handle
		disp uint32
	)
	r0, _, _ := procRegCreateKeyEx.Call(
		uintptr(hkeyCurrentUser),
		uintptr(unsafe.Pointer(p)),
		0, 0, 0, keyWrite, 0,
		uintptr(unsafe.Pointer(&h)),
		uintptr(unsafe.Pointer(&disp)),
	)
	if r0 != 0 {
		return 0, 0, syscall.Errno(r0)
	}
	return h, disp, nil
}

// registrySetString 写一个 REG_SZ 值——专门用于验证 registryGetDWORD
// 对「同名错误类型」的拒绝（ocr M6）。
func registrySetString(h syscall.Handle, name, value string) error {
	np, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	vb, err := syscall.UTF16FromString(value)
	if err != nil {
		return err
	}
	r0, _, _ := procRegSetValueEx.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(np)),
		0,
		uintptr(regSZ),
		uintptr(unsafe.Pointer(&vb[0])),
		uintptr(len(vb)*2),
	)
	if r0 != 0 {
		return syscall.Errno(r0)
	}
	return nil
}

// registryDeleteKey 递归删除测试键（RegDeleteTreeW 只在测试里用）。
func registryDeleteKey(sub string) error {
	p, err := syscall.UTF16PtrFromString(sub)
	if err != nil {
		return err
	}
	r0, _, _ := procRegDeleteKey.Call(uintptr(hkeyCurrentUser), uintptr(unsafe.Pointer(p)))
	if r0 != 0 {
		return syscall.Errno(r0)
	}
	return nil
}
