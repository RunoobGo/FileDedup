//go:build windows

package scanner

import (
	"os"
	"syscall"
	"unsafe"

	"filededup/internal/model"
)

// Windows 没有 inode；按需解析（决策 11）：仅候选组内调用 ResolveKey。
// 通过 GetFileInformationByHandle 获取 卷序列号 + 64 位文件索引。

type byHandleFileInformation struct {
	VolumeSerialNumber uint32
	FileSizeHigh       uint32
	FileSizeLow        uint32
	NumberOfLinks      uint32
	FileIndexHigh      uint32
	FileIndexLow       uint32
}

var (
	modkernel32                    = syscall.NewLazyDLL("kernel32.dll")
	procGetFileInformationByHandle = modkernel32.NewProc("GetFileInformationByHandle")
)

// keyFromInfo Windows：遍历阶段不解析（避免每文件开句柄）。
func keyFromInfo(path string, info os.FileInfo) model.FileKey {
	return model.FileKey{Resolved: false}
}

// ResolveKey 按需解析物理文件标识（候选组内调用，同一次可与预筛共用句柄的调度由上层负责）。
func ResolveKey(e *model.FileEntry) model.FileKey {
	if e.Key.Resolved {
		return e.Key
	}
	h, err := syscall.Open(e.Path, syscall.O_RDONLY, 0)
	if err != nil {
		return model.FileKey{}
	}
	defer syscall.CloseHandle(syscall.Handle(h))
	var info byHandleFileInformation
	r1, _, _ := procGetFileInformationByHandle.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(&info)),
	)
	if r1 == 0 {
		return model.FileKey{}
	}
	e.Key = model.FileKey{
		VolumeID:  uint64(info.VolumeSerialNumber),
		FileIndex: uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow),
		Resolved:  true,
	}
	return e.Key
}
