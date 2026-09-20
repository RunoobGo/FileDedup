//go:build windows

package ops

import (
	"errors"
	"syscall"
)

// winErrorNotSameDevice 是 Win32 ERROR_NOT_SAME_DEVICE 的**真值 17（0x11）**。
//
// AS-H5（2026-09-20 全仓审计，第 7 篇 §三）：修正前写作 0x11E(286)，注释也照抄了
// 这个错值。真值实证取自 Go 自带源码
// $GOROOT/src/cmd/vendor/golang.org/x/sys/windows/zerrors_windows.go:170
// `ERROR_NOT_SAME_DEVICE syscall.Errno = 17`。
//
// 为什么判错就等于兜底失效：Go 的 os.Rename 在 Windows 直接包 MoveFileExW 的
// 原始 errno（不翻译成 EXDEV），所以跨卷 rename 返回给上层的正是 17；而 Go 的
// syscall.EXDEV 属于 Winsock APPLICATION_ERROR 段的大数，永远不等于 17。
// 于是修正前 isCrossDevice 在 Windows 恒 false → move.go 的「复制+删源」分支
// 永不进入 → 跨硬盘移动（本产品核心场景之一）与 undoMove 必然失败。
// 失败是响亮的（不是静默丢数据），但主路径不可用。
const winErrorNotSameDevice = syscall.Errno(17)

// isCrossDeviceExtra 在 Windows 上补充判定跨卷错误。
// unix 的 EXDEV 由 isCrossDevice 主判据覆盖，本函数只负责 Windows 原生错误码。
func isCrossDeviceExtra(err error) bool {
	return errors.Is(err, winErrorNotSameDevice)
}
