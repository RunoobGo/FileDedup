//go:build windows

package ops

import (
	"syscall"
	"unsafe"
)

// 极简注册表只读访问（advapi32）。
//
// 为什么不引 golang.org/x/sys/windows/registry：
// 本项目刻意保持**零第三方依赖**（见 docs/02 决策：跨平台自研 trash、
// 不依赖 gio 等外部工具）。x/sys 虽属官方扩展库，但为读一个 DWORD 引入
// 整个模块并不划算，且会进入 go.mod 影响离线构建。这里直接用 syscall。

const (
	hkeyCurrentUser = 0x80000001
	keyRead         = 0x20019 // KEY_READ（含 KEY_QUERY_VALUE | KEY_ENUMERATE_SUB_KEYS）
)

var (
	advapi32          = syscall.NewLazyDLL("advapi32.dll")
	procRegOpenKeyEx  = advapi32.NewProc("RegOpenKeyExW")
	procRegQueryValue = advapi32.NewProc("RegQueryValueExW")
	procRegCloseKey   = advapi32.NewProc("RegCloseKey")
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
	return data, nil
}
