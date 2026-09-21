//go:build windows

package cloudfile

import (
	"os"
	"syscall"
)

// Current 见 cloudfile_darwin.go 的同名说明。
const Current = PlatformWindows

// flagsOf 读 dwFileAttributes。$GOROOT/src/os/types_windows.go 的
// newFileStatFromWin32finddata 把 FindFirstFile 的属性原样带进 fileStat，
// Sys() 再回吐成 *syscall.Win32FileAttributeData —— 所以 ReadDir 路径上
// 这个数**已经在手**，不必调 GetFileAttributesEx。
//
// ★ 真机未兑现：占位文件的 RECALL 位是否真的出现在 FindFirstFile 的
// dwFileAttributes 里，是读 GOROOT 的推论而非本机读数（无 Windows runner、
// 无 OneDrive 账号）。判据本身（"读不到→不跳过"）由无 tag 层钉住，不依赖真机。
func flagsOf(info os.FileInfo) (uint32, bool) {
	if info == nil {
		return 0, false
	}
	st, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return 0, false
	}
	return st.FileAttributes, true
}
