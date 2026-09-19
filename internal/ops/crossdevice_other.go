//go:build !windows

package ops

// isCrossDeviceExtra 在非 Windows 平台恒为 false：跨卷仅由 EXDEV 判定。
func isCrossDeviceExtra(err error) bool { return false }
