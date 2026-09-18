//go:build windows

package ops

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

// SHFileOperationW + FOF_ALLOWUNDO：移入回收站（批量，双 \0 结尾路径列表）。

const (
	foDelete          = 3
	fofAllowUndo      = 0x40
	fofNoConfirmation = 0x10
	fofSilent         = 0x4
)

// GetDriveTypeW 返回值（winbase.h）
const (
	driveNoRootDir = 1
	driveRemovable = 2
	driveFixed     = 3
	driveRemote    = 4
	driveCDROM     = 5
	driveRamdisk   = 6
)

type shFileOpStruct struct {
	hwnd                  uintptr
	wFunc                 uint32
	pFrom                 *uint16
	pTo                   *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

var (
	shell32             = syscall.NewLazyDLL("shell32.dll")
	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	procSHFileOperation = shell32.NewProc("SHFileOperationW")
	procGetDriveType    = kernel32.NewProc("GetDriveTypeW")
)

// unsafeDriveReason 判定路径所在卷能否保证「回收站可还原」（H3）。
//
// FOF_ALLOWUNDO 是尽力而为：网络盘（DRIVE_REMOTE）、可移动盘、光碟、RAM
// 盘上没有系统回收站，Shell 会静默永久删除且返回成功——"移入回收站"
// 对用户承诺可还原，实际不可恢复，属于数据丢失级缺陷。现在这些卷直接
// 报错拒绝，由执行器逐项记 Failed 并在提示中建议改用「移动」。
// 残余风险：固定盘单文件超过回收站配额时 Shell 也可能直接删除，
// 配额可编程查询的接口冗杂（IQueryRBInfo/SEP2），暂以文档提示兜底。
func unsafeDriveReason(p string) string {
	if strings.HasPrefix(p, `\\`) {
		return "网络位置（UNC 路径）没有系统回收站"
	}
	if len(p) < 3 || p[1] != ':' || (p[2] != '\\' && p[2] != '/') {
		return "无法识别所在卷"
	}
	root, err := syscall.UTF16PtrFromString(p[:2] + `\)`)
	if err != nil {
		return "路径编码失败"
	}
	r, _, _ := procGetDriveType.Call(uintptr(unsafe.Pointer(root)))
	switch int(r) {
	case driveFixed:
		return "" // 有回收站，可撤销
	case driveRemote:
		return "网络驱动器没有系统回收站"
	case driveRemovable:
		return "可移动磁盘没有系统回收站"
	case driveCDROM:
		return "光碟为只读介质"
	case driveRamdisk:
		return "RAM 磁盘无回收站"
	case driveNoRootDir:
		return "根目录不存在"
	default:
		return fmt.Sprintf("卷类型不可回收（GetDriveType=%d）", r)
	}
}

func defaultTrash(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	// 先全量预检再动手：任一文件位于不可回收卷则整批拒绝。
	// 执行器的批量失败回退（C6）会逐个重试，可回收文件逐项成功、
	// 不可回收文件逐项 Failed，与直接逐项分发的结果等价且不会半途丢失。
	var rejected []string
	for _, p := range paths {
		if why := unsafeDriveReason(p); why != "" {
			rejected = append(rejected, fmt.Sprintf("%s（%s）", p, why))
		}
	}
	if len(rejected) > 0 {
		return fmt.Errorf("%d 个文件所在卷无回收站，已拒绝以防静默永久删除；请改用「移动」或自行确认后再「永久删除」。首个: %s",
			len(rejected), rejected[0])
	}
	// 注意：SHFileOperation 不支持 \\?\ 前缀，使用普通路径
	var from []uint16
	for _, p := range paths {
		from = append(from, utf16FromString(p)...)
		from = append(from, 0)
	}
	from = append(from, 0) // 列表整体再以 \0 结尾

	op := shFileOpStruct{
		wFunc:  foDelete,
		pFrom:  &from[0],
		fFlags: fofAllowUndo | fofNoConfirmation | fofSilent,
	}
	r0, _, _ := procSHFileOperation.Call(uintptr(unsafe.Pointer(&op)))
	if r0 != 0 {
		return fmt.Errorf("SHFileOperation 错误码 %d", r0)
	}
	if op.fAnyOperationsAborted != 0 {
		return fmt.Errorf("操作被系统中止")
	}
	return nil
}

func utf16FromString(s string) []uint16 {
	return syscall.StringToUTF16(s)
}
