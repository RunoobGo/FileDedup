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
	procCreateFileW                = modkernel32.NewProc("CreateFileW")
)

const (
	c4FullShare      = syscall.FILE_SHARE_READ | syscall.FILE_SHARE_WRITE | syscall.FILE_SHARE_DELETE
	c4BackupSemant   = 0x02000000 // FILE_FLAG_BACKUP_SEMANTICS
	c4OpenExisting   = 3          // OPEN_EXISTING
	c4GenericRead    = 0x80000000
	c4FileAttrNormal = 0x00000080
)

// keyFromInfo Windows：遍历阶段不解析（避免每文件开句柄）。
func keyFromInfo(path string, info os.FileInfo) model.FileKey {
	return model.FileKey{Resolved: false}
}

// ResolveKey 按需解析物理文件标识（候选组内调用，同一次可与预筛共用句柄的调度由上层负责）。
//
// C4：修正前直接 syscall.Open，任何打开失败（共享冲突/删除挂起/权限）都静默
// 返回空 key → 硬链接对不去重 → 重复计数、Reclaimable 虚高（删硬链接兄弟并不
// 释放空间）。现改为两段策略：标准 os.Open 优先（Go 管理句柄，flags 与运行时
// 一致）；失败时以「全共享模式 + BACKUP_SEMANTICS」兜底重开，尽力不放弃去重。
// 两段都失败才返回未解析（此时已无更安全的身份来源）。
func ResolveKey(e *model.FileEntry) model.FileKey {
	if e.Key.Resolved {
		return e.Key
	}
	k, ok := resolveByHandle(e.Path)
	if !ok {
		return model.FileKey{}
	}
	e.Key = k
	return e.Key
}

func resolveByHandle(path string) (model.FileKey, bool) {
	// 第一选择：标准打开（句柄生命周期由 runtime 管理，自动关闭）
	if f, err := os.Open(path); err == nil {
		defer f.Close()
		return infoFromHandle(uintptr(f.Fd()))
	}
	// C4 兜底：全共享 + BACKUP_SEMANTICS 重开，覆盖被占用的文件
	p16, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return model.FileKey{}, false
	}
	h, _, callErr := procCreateFileW.Call(
		uintptr(unsafe.Pointer(p16)),
		c4GenericRead,
		c4FullShare,
		0,
		c4OpenExisting,
		c4FileAttrNormal|c4BackupSemant,
		0,
	)
	if h == uintptr(syscall.InvalidHandle) || h == 0 {
		_ = callErr // LazyProc.Call 恒返回非 nil err；失败以句柄值判定
		return model.FileKey{}, false
	}
	defer syscall.CloseHandle(syscall.Handle(h))
	return infoFromHandle(h)
}

func infoFromHandle(h uintptr) (model.FileKey, bool) {
	var info byHandleFileInformation
	r1, _, _ := procGetFileInformationByHandle.Call(
		h,
		uintptr(unsafe.Pointer(&info)),
	)
	if r1 == 0 {
		return model.FileKey{}, false
	}
	return model.FileKey{
		VolumeID:  uint64(info.VolumeSerialNumber),
		FileIndex: uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow),
		Resolved:  true,
	}, true
}
