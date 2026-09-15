//go:build windows

package ops

import (
	"fmt"
	"syscall"
	"unsafe"
)

// SHFileOperationW + FOF_ALLOWUNDO：移入回收站（批量，双 \0 结尾路径列表）。

const (
	foDelete         = 3
	fofAllowUndo     = 0x40
	fofNoConfirmation = 0x10
	fofSilent        = 0x4
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
	shell32            = syscall.NewLazyDLL("shell32.dll")
	procSHFileOperation = shell32.NewProc("SHFileOperationW")
)

func defaultTrash(paths []string) error {
	if len(paths) == 0 {
		return nil
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
