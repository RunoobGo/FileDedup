//go:build !darwin && !windows

package cloudfile

import "os"

// Current 见 cloudfile_darwin.go 的同名说明。与 internal/sysguard、internal/fscase
// 同一口径：非 windows/darwin 一律按 Linux 处理。
const Current = PlatformLinux

// Linux 及其变体：**没有统一的占位语义**，恒不判定（设计稿 §4.5）。
// rclone/gvfs/lookback 各自一套，猜一条判据的代价是把普通文件吞出语料。
// 与 internal/fsid/fsid_other.go、internal/realbytes/realbytes_other.go 同一处置。
func flagsOf(info os.FileInfo) (uint32, bool) {
	return 0, false
}
