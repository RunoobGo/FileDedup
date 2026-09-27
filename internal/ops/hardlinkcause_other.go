//go:build !windows

package ops

import "fmt"

// 非 Windows 上不需要按错误码分派归因：`os.Link` 交回的就是 `syscall.EXDEV` /
// `EPERM` / `EACCES` 这一类**自带语义**的 errno，Go 的文本已经是"cross-device link
// not permitted"，再叠一层中文括号只会有两种下场——要么与原文重复，要么像 Windows 那样
// 把猜测写进括号（M273 立的正是后者）。所以这里只加前缀、不改归因。
func hardlinkLinkError(err error) error {
	return fmt.Errorf("硬链接失败: %w", err)
}
