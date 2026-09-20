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

// Win32 ABI 契约：结构体必须与 C 侧定义逐字节等值。尺寸一偏离，proc.Call 就会
// 朝 Go 变量里多写/少写字节——少写只是读错字段，多写直接越界踩坏栈。
// 2026-09-19 CI 发现 scanner 包曾另造过一份 24 字节的精简版（把卷号读成文件属性、
// 把索引读成时间戳），故此处钉死尺寸，并要求全仓只留这一份布局。
var (
	// BY_HANDLE_FILE_INFORMATION：7 个 DWORD + 3 个 FILETIME = 52 字节。
	_ [52 - unsafe.Sizeof(byHandleInfo{})]byte
	_ [unsafe.Sizeof(byHandleInfo{}) - 52]byte
	// FILE_BASIC_INFO：4 个 LARGE_INTEGER + 1 个 DWORD = 36 字节，8 字节对齐补到 40。
	_ [40 - unsafe.Sizeof(basicInfo{})]byte
	_ [unsafe.Sizeof(basicInfo{}) - 40]byte
)

var (
	modkernel32                      = syscall.NewLazyDLL("kernel32.dll")
	procGetFileInformationByHandle   = modkernel32.NewProc("GetFileInformationByHandle")
	procGetFileInformationByHandleEx = modkernel32.NewProc("GetFileInformationByHandleEx")
	procCreateFileW                  = modkernel32.NewProc("CreateFileW")
)

const (
	// win32EpochDiff 1601-01-01 → 1970-01-01 之间的 100ns 间隔数。
	win32EpochDiff = 116444736000000000
	// fileBasicInfoClass FILE_INFO_BY_HANDLE_CLASS.FileBasicInfo。
	fileBasicInfoClass = 1

	// CreateFileW 参数常量（取自 Win32 头文件）。
	genericRead        = 0x80000000
	fullShare          = syscall.FILE_SHARE_READ | syscall.FILE_SHARE_WRITE | syscall.FILE_SHARE_DELETE
	openExisting       = 3
	fileAttrNormal     = 0x80
	fileFlagBackupSem  = 0x02000000
	fileFlagOpenRepars = 0x00200000 // FILE_FLAG_OPEN_REPARSE_POINT：不跟随重解析点
	invalidHandleValue = ^uintptr(0)
)

// fromInfo Windows：os 层伪造的 Stat_t 跨重命名不稳定且无卷号/索引，
// 不足以支撑身份判定 → 未解析（调用方退回内容级证据）。
func fromInfo(info os.FileInfo) ID { return ID{} }

// FromPathNoFollow 取 path **自身**的物理身份，不跟随符号链接/重解析点。
//
// 为什么必须有它（2026-09-19 关联隐患修复）：
// Windows 的 Lstat 产物经 fromInfo 恒返回未解析 ID，导致所有
// 「FromFileInfo(Lstat(...))」形式的身份校验在这个平台上**静默失效**；
// 而直接用 os.Open + FromFile 又会**跟随**符号链接，使"路径被换成链接"
// 这类替换检测不到，反而制造新的绕过面。
//
// 这里用 CreateFileW 显式带 FILE_FLAG_OPEN_REPARSE_POINT：
//   - 不跟随链接 → 取到的就是 path 位置上的那个对象本身；
//   - 走句柄查询 → 拿得到真实的 (卷序列号, 文件索引)。
//
// 语义与 unix 的 lstat 对齐，调用方因此不必分平台。
//
// 打不开（不存在/权限/被独占）时返回 error；打开成功但卷不给稳定索引
// （FAT/exFAT 恒 0）时返回 Resolved == false 的 ID——两种情形调用方需分开处置。
func FromPathNoFollow(path string) (ID, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return ID{}, err
	}
	h, _, callErr := procCreateFileW.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(genericRead),
		uintptr(fullShare),
		0,
		uintptr(openExisting),
		// BACKUP_SEMANTICS 用于能打开目录；OPEN_REPARSE_POINT 用于不跟随链接。
		uintptr(fileAttrNormal|fileFlagBackupSem|fileFlagOpenRepars),
		0,
	)
	if h == invalidHandleValue {
		return ID{}, callErr
	}
	defer syscall.CloseHandle(syscall.Handle(h))
	return fromHandle(h), nil
}

// FromHandle Windows 专属：从调用方已持有的原生句柄取身份。
//
// 给 scanner 的 C4 兜底路径用——那条路用 CreateFileW(+BACKUP_SEMANTICS) 打开被占用
// 文件，拿不到 *os.File。身份查询只此一份实现：BY_HANDLE_FILE_INFORMATION 的字段
// 布局禁止在别处复制（复制一份精简版就会把卷号读成文件属性、把索引读成时间戳）。
func FromHandle(h uintptr) ID { return fromHandle(h) }

// FromPath 取 path **所指向对象**的物理身份，**跟随**符号链接。
//
// 与 FromPathNoFollow 的唯一差异是 CreateFileW 的 flags 里**不带**
// FILE_FLAG_OPEN_REPARSE_POINT：带则停在重解析点（拿到链接自身），
// 不带则由 I/O 管理器穿透到目标。
//
// 2026-09-20（跨卷软链接合并）：这是软链接终局复核的必需能力。
// 若照搬 FromPathNoFollow 去校验"链接是否指向保留源"，取到的会是链接自身
// 的 (卷序列号, 文件索引)，与保留源恒不相等——每次合并都会被误判为失败。
//
// 悬空链接：CreateFileW 直接失败（ERROR_FILE_NOT_FOUND / ERROR_PATH_NOT_FOUND），
// 返回 error；调用方据此报"目标不可达"。
//
// 注：FILE_FLAG_BACKUP_SEMANTICS 保留——目标可能是目录（虽然本应用只处理
// 普通文件），且该标志对普通文件无副作用。
func FromPath(path string) (ID, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return ID{}, err
	}
	h, _, callErr := procCreateFileW.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(genericRead),
		uintptr(fullShare),
		0,
		uintptr(openExisting),
		// 有意**不加** fileFlagOpenRepars：要的就是穿透链接取目标。
		uintptr(fileAttrNormal|fileFlagBackupSem),
		0,
	)
	if h == invalidHandleValue {
		return ID{}, callErr
	}
	defer syscall.CloseHandle(syscall.Handle(h))
	return fromHandle(h), nil
}

// fromFile 句柄查询 (卷号, 文件索引[, change time])。
// 主查询失败、或卷不给稳定索引（FAT/exFAT 恒 0）都返回未解析：
// 宁可用旧的采样兜底，也不用一个可能人人都相同的"身份"去放行命中。
// change time 取不到只丢这一重证据（CtimeNs=0），不影响身份成立。
func fromFile(f *os.File) ID { return fromHandle(f.Fd()) }

func fromHandle(h uintptr) ID {
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
