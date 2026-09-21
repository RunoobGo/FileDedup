//go:build windows

package realbytes

import (
	"hash/fnv"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

// Windows 平台的实占取自 GetCompressedFileSizeW——它是 Win32 里唯一同时把
// **NTFS 压缩**和**稀疏文件**折算成"真正落盘的字节数"的 API
// （st_size 一类语义对应的是逻辑大小，FILE_STANDARD_INFORMATION 的
// AllocationSize 不反映压缩）。
//
// ★ 与设计稿 §3.1 的一处修正（2026-09-21 实施时发现）：原写"挂在 internal/fsid
// 已打开的句柄上"。这个前提在扫描路径上**不成立**——Windows 的遍历阶段刻意
// 不开句柄（见 internal/scanner/filekey_windows.go：keyFromInfo 每文件开句柄的
// 代价过高，故身份按需解析）。所以这里改用**按路径**的 GetCompressedFileSizeW
// （该 API 本身就接受路径，内部以 FILE_READ_ATTRIBUTES 打开），代价是
// Windows 上每个"通过过滤器的候选文件"多一次元数据查询。这是本轮唯一的
// 平台不对称开销，已在 04 §6.9.3 记为待评估项。
//
// ★ 本文件的真机行为**未兑现验证**：darwin/linux 主门禁只能做到
// `GOOS=windows go vet` 的交叉编译检查。"读不到→回退并标 known=false"
// 这条兜底由无 tag 层（From + U1）钉住，不依赖真机。

var (
	modkernel32                = syscall.NewLazyDLL("kernel32.dll")
	procGetCompressedFileSizeW = modkernel32.NewProc("GetCompressedFileSizeW")
)

// invalidFileSize 即 Win32 的 INVALID_FILE_SIZE（0xFFFFFFFF）——它同时也是
// "低 32 位恰好全是 1"的合法尺寸，故必须靠 GetLastError 二选一，不能只看返回值。
const invalidFileSize = 0xFFFFFFFF

func Reported(path string, info os.FileInfo) (uint64, bool) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, false // 含 NUL 等非法路径：不猜，走回退
	}
	var high uint32
	low, _, errno := procGetCompressedFileSizeW.Call(
		uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&high)))
	if low == invalidFileSize && errno != nil {
		// 常见失败：目录、ACL 拒绝读属性、超过 MAX_PATH 且未加 \\?\ 前缀。
		// 一律回退成逻辑大小 + known=false——长路径场景下宁可数字粗一点，
		// 也不能让扫描报错。
		return 0, false
	}
	return uint64(high)<<32 | uint64(low), true
}

// VolumeID windows：filepath.VolumeName 给出 "C:" / "\\server\share" /
// "\\?\Volume{…}" 一类稳定前缀，FNV-1a 成 uint64 只为与 unix 的 st_dev
// 共用一套键类型（M28）。空前缀（相对路径一类）⇒ (0,false)——拿不到卷标识
// 就永远不给卷级证据（fail-closed）。哈希冲突概率约 2⁻⁶⁴，方向是"两卷证据
// 混池"，见设计稿 §11.5-6。
func VolumeID(path string, info os.FileInfo) (uint64, bool) {
	vol := filepath.VolumeName(path)
	if vol == "" {
		return 0, false
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(vol))
	return h.Sum64(), true
}
