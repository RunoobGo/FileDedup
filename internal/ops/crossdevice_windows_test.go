//go:build windows

package ops

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"testing"
)

// AS-H5（2026-09-20 全仓审计，第 7 篇 §三）：Windows 跨卷错误码写错。
//
// crossdevice_windows.go 把 ERROR_NOT_SAME_DEVICE 写成 0x11E(286)，真值是
// **17（0x11）**——实证取自 Go 自带源码
// $GOROOT/src/cmd/vendor/golang.org/x/sys/windows/zerrors_windows.go:170
// `ERROR_NOT_SAME_DEVICE syscall.Errno = 17`。Go 的 os.Rename 在 Windows 直接
// 包 MoveFileExW 的原始 errno，所以真实故障码就是 17；而 syscall.EXDEV 属于
// APPLICATION_ERROR 段的大数，也不会等于 17。
//
// 后果：isCrossDevice 在 Windows 恒 false → move.go 的「复制+删源」兜底分支
// 永不进入 → 跨硬盘移动（本产品核心场景之一）与 undoMove 必然失败。
//
// 这是第 6 篇那条教训的同构复发：「写成常量、在错误的平台上测」。
// move_p2_test.go 确实测了 isCrossDevice，但在 darwin 上跑的是
// crossdevice_other.go 的 `return false` 桩，Windows 常量从未被执行。
// 本文件属"本机不可红、只能由 windows CI 腿兑现"的那一类（04 §6.8.0 约束 5）。

// sdkErrorNotSameDevice Win32 ERROR_NOT_SAME_DEVICE 真值（出处见文件头）。
//
// 这里刻意**不复用**实现里的常量名与取值：断言的说服力必须来自与实现分离的
// 独立来源。也不引 golang.org/x/sys/windows 只为拿这个常量——它在本模块是
// indirect 依赖，为一条断言把它提成 direct 不值当。
const sdkErrorNotSameDevice = syscall.Errno(17)

func TestCrossDeviceErrnoMatchesSDKConstant(t *testing.T) {
	if sdkErrorNotSameDevice != 0x11 {
		t.Fatalf("测试自身构造错误：ERROR_NOT_SAME_DEVICE 应为 0x11，写成 %d", sdkErrorNotSameDevice)
	}
	if sdkErrorNotSameDevice == syscall.Errno(0x11E) {
		t.Fatal("测试自身构造错误：0x11E 不是 ERROR_NOT_SAME_DEVICE")
	}
	if !isCrossDeviceExtra(sdkErrorNotSameDevice) {
		t.Fatal("isCrossDeviceExtra 未认得 SDK 真值 17")
	}
}

// TestIsCrossDeviceAcceptsWindowsNotSameDevice 真实故障码必须被判为跨卷。
// 修正前 isCrossDeviceExtra 认 0x11E，这里判 false → windows job 红。
func TestIsCrossDeviceAcceptsWindowsNotSameDevice(t *testing.T) {
	le := &os.LinkError{Op: "rename", Err: sdkErrorNotSameDevice}
	if !isCrossDevice(le) {
		t.Fatalf("isCrossDevice(ERROR_NOT_SAME_DEVICE=%d) = false：%d 是 Windows 上跨卷 rename 的"+
			"实际返回码，判否会让复制+删源兜底永不进入，跨硬盘移动与 undoMove 必然失败（AS-H5）",
			sdkErrorNotSameDevice, sdkErrorNotSameDevice)
	}
	// 包装一层仍要认（调用方普遍 fmt.Errorf("%w") 过）
	if !isCrossDevice(fmt.Errorf("移动失败: %w", le)) {
		t.Fatal("包装后的 ERROR_NOT_SAME_DEVICE 未被识别")
	}
}

// TestIsCrossDeviceRejectsBogus0x11E 写错的那个码不得再被当跨卷：
// 286 在 Win32 是 ERROR_CREATE_FAILED 段，与"不同卷"无关；放行它会让
// 真正的失败（如策略拦截）被误判成跨卷并走复制+删源，风险方向相反。
func TestIsCrossDeviceRejectsBogus0x11E(t *testing.T) {
	if isCrossDevice(&os.LinkError{Err: syscall.Errno(0x11E)}) {
		t.Fatal("0x11E 不应被判为跨卷错误码")
	}
}

// TestIsCrossDeviceRejectsUnrelatedErrnos 负控制收窄到本缺陷相关项，
// 其余常见码一律不认（防"顺手放宽"把权限错误也吞进复制+删源）。
func TestIsCrossDeviceRejectsUnrelatedErrnos(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
	}{
		{"EPERM", &os.LinkError{Err: syscall.EPERM}},
		{"EACCES", &os.LinkError{Err: syscall.EACCES}},
		{"ERROR_ACCESS_DENIED(5)", &os.LinkError{Err: syscall.Errno(5)}},
		{"ERROR_FILE_EXISTS(80)", &os.LinkError{Err: syscall.Errno(80)}},
		{"ERROR_SHARING_VIOLATION(32)", &os.LinkError{Err: syscall.Errno(32)}},
		{"非 LinkError", errors.New("boom")},
	} {
		if isCrossDevice(c.err) {
			t.Errorf("%s: 不应判为跨卷", c.name)
		}
	}
}
