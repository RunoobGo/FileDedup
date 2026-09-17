//go:build windows

package ops

import (
	"errors"
	"syscall"
)

// isCrossDeviceExtra 在 Windows 上补充判定跨卷错误。
//
// Windows 的跨卷 rename 返回 ERROR_NOT_SAME_DEVICE(0x11E)，而非 unix 的 EXDEV(0x12)。
// 新实现把 isCrossDevice 收紧为仅认 syscall.EXDEV，会丢掉 Windows 分支，
// 导致 Windows 上跨卷 move 无法走「复制+删源」兜底路径而直接失败。这里补回该分支。
func isCrossDeviceExtra(err error) bool {
	return errors.Is(err, syscall.Errno(0x11E))
}
