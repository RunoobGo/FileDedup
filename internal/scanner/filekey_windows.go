//go:build windows

package scanner

import (
	"os"
	"syscall"
	"unsafe"

	"filededup/internal/fsid"
	"filededup/internal/model"
)

// Windows 没有 inode：按需解析（决策 11）——仅候选组内调用 ResolveKey，
// 身份取句柄查询的「卷序列号 + 64 位文件索引」，实现统一在 internal/fsid
// （它的 byHandleInfo 与 Win32 定义逐字节对齐，并有编译期尺寸断言兜底）。
//
// 2026-09-19 修正：本文件此前自带一份 24 字节的 byHandleFileInformation，
// 少了 dwFileAttributes 与三个 FILETIME——字段整体前移 28 字节，于是
// 卷号读成文件属性、文件索引读成 (最后访问, 最后写入) 时间戳，且 Win32 朝这个
// 24 字节变量写入 52 字节。后果不是崩溃就是"同时刻创建的同尺寸文件键值全相同"，
// 阶段 1.5 据此把不同文件当硬链接合并，整组重复静默消失（windows CI 实测）。

var (
	modkernel32     = syscall.NewLazyDLL("kernel32.dll")
	procCreateFileW = modkernel32.NewProc("CreateFileW")
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
		return keyFromID(fsid.FromFile(f))
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
	return keyFromID(fsid.FromHandle(h))
}

// keyFromID fsid.ID → 硬链接去重用的 FileKey（丢掉 ctime：本阶段只问"是否同一文件"）。
// 未解析一律传 false，交由调用方退回内容级证据。
func keyFromID(id fsid.ID) (model.FileKey, bool) {
	if !id.Resolved {
		return model.FileKey{}, false
	}
	return model.FileKey{VolumeID: id.Dev, FileIndex: id.Ino, Resolved: true}, true
}
