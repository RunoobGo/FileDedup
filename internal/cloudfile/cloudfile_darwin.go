//go:build darwin

package cloudfile

import (
	"os"
	"syscall"
)

// Current 本包唯一按 GOOS 分流的地方，判据在平台位之外全部无 tag。
const Current = PlatformDarwin

// flagsOf 读 st_flags。值就在遍历期 de.Info() 拿到的那份 Stat_t 里，
// **不发额外 syscall**（本机真读数与对照见 cloudfile.go 头与设计稿 §4.0 E5~E7）。
func flagsOf(info os.FileInfo) (uint32, bool) {
	if info == nil {
		return 0, false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return st.Flags, true
}
