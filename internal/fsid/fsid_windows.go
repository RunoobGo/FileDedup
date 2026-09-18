//go:build windows

package fsid

import (
	"os"
	"syscall"
	"unsafe"
)

// Windows 没有 inode：物理身份取自句柄查询的「卷序列号 + 64 位文件索引」
// （NTFS/ReFS 上索引随文件走、rename 不变，硬链接共享同一索引，语义等价 unix dev/ino）。
// 这两项只在 BY_HANDLE_FILE_INFORMATION 里，Lstat 产物拿不到，故 FromFileInfo
// 仍返回未解析、FromFile 才解析得动。

// byHandleInfo 对应 Win32 BY_HANDLE_FILE_INFORMATION（逐字段对齐，勿增删字段：
// 多一个 8 字节就会把卷号读成文件大小）。change time 不在此结构里，见 basicInfo。
type byHandleInfo struct {
	FileAttributes     uint32
	CreationTime       syscall.Filetime
	LastAccessTime     syscall.Filetime
	LastWriteTime      syscall.Filetime
	VolumeSerialNumber uint32
	FileSizeHigh       uint32
	FileSizeLow        uint32
	NumberOfLinks      uint32
	FileIndexHigh      uint32
	FileIndexLow       uint32
}

// basicInfo 对应 FILE_BASIC_INFO——唯一能取到 change time 的句柄查询
// （BY_HANDLE_FILE_INFORMATION 与 GetFileTime 都只有创建/访问/写入三个时间）。
type basicInfo struct {
	CreationTime   int64
	LastAccessTime int64
	LastWriteTime  int64
	ChangeTime     int64
	FileAttributes uint32
	_              [4]byte // 8 字节对齐补位
}

var (
	modkernel32                      = syscall.NewLazyDLL("kernel32.dll")
	procGetFileInformationByHandle   = modkernel32.NewProc("GetFileInformationByHandle")
	procGetFileInformationByHandleEx = modkernel32.NewProc("GetFileInformationByHandleEx")
)

const (
	// win32EpochDiff 1601-01-01 → 1970-01-01 之间的 100ns 间隔数。
	win32EpochDiff = 116444736000000000
	// fileBasicInfoClass FILE_INFO_BY_HANDLE_CLASS.FileBasicInfo。
	fileBasicInfoClass = 1
)

// fromInfo Windows：os 层伪造的 Stat_t 跨重命名不稳定且无卷号/索引，
// 不足以支撑身份判定 → 未解析（调用方退回内容级证据）。
func fromInfo(info os.FileInfo) ID { return ID{} }

// fromFile 句柄查询 (卷号, 文件索引[, change time])。
// 主查询失败、或卷不给稳定索引（FAT/exFAT 恒 0）都返回未解析：
// 宁可用旧的采样兜底，也不用一个可能人人都相同的"身份"去放行命中。
// change time 取不到只丢这一重证据（CtimeNs=0），不影响身份成立。
func fromFile(f *os.File) ID {
	h := uintptr(f.Fd())
	var info byHandleInfo
	if r, _, _ := procGetFileInformationByHandle.Call(h, uintptr(unsafe.Pointer(&info))); r == 0 {
		return ID{}
	}
	index := uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow)
	if index == 0 || info.VolumeSerialNumber == 0 {
		return ID{} // 索引/卷号缺位：这份"身份"没有区分力
	}
	var ctimeNs int64
	var bi basicInfo
	if r, _, _ := procGetFileInformationByHandleEx.Call(h, fileBasicInfoClass,
		uintptr(unsafe.Pointer(&bi)), unsafe.Sizeof(bi)); r != 0 {
		ctimeNs = ticksToUnixNs(bi.ChangeTime)
	}
	return ID{
		Dev:      uint64(info.VolumeSerialNumber),
		Ino:      index,
		CtimeNs:  ctimeNs,
		Resolved: true,
	}
}

// ticksToUnixNs 100ns 刻度（自 1601）→ Unix 纳秒；0（未设置）保持 0。
func ticksToUnixNs(ticks int64) int64 {
	if ticks == 0 {
		return 0
	}
	return (ticks - win32EpochDiff) * 100
}
