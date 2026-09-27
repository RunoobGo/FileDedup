//go:build windows

package ops

// M273：`os.Link` 失败分支的归因。真机读数（exFAT 与 FAT32 同形）：
// `CreateHardLinkW` 返回 0、GetLastError = **1**（ERROR_INVALID_FUNCTION），
// Go 侧拿到 `errno=1` 文本 "Incorrect function."——**不是** docs/05 W5-3 预期的 50。
// 而产品当时那句「硬链接失败（可能跨卷或权限）」把**同卷、非 NTFS**这一形状
// 说成了"跨卷或权限"：用户在 exFAT U 盘上照着括号去查权限，方向整个偏掉。
//
// ★ 真正说对了原因的文案（`verifyHardlinked` 那句"该卷可能不支持硬链接"）挂在
//   **收尾复核**那一支，而失败发生在更早的 `os.Link` ⇒ 那一支在这两类卷上不可达。
//   所以修的是产生处，不是把复核那句搬过来当兜底。

import (
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
)

func m273LinkErr(errno syscall.Errno) error {
	return &os.LinkError{Op: "link", Old: `E:\w\keep.bin`, New: `E:\w\dup.bin.fdd-tmp`, Err: errno}
}

// 卷型不支持那一档必须被点名，且**不得**再提"权限"。
func TestM273UnsupportedVolumeIsNamedAndDoesNotBlamePermission(t *testing.T) {
	for _, c := range []struct {
		name  string
		errno syscall.Errno
	}{
		{"ERROR_INVALID_FUNCTION(1) 真机实测值", syscall.Errno(1)},
		{"ERROR_NOT_SUPPORTED(50) 清单预期值", syscall.Errno(50)},
	} {
		err := hardlinkLinkError(m273LinkErr(c.errno))
		msg := err.Error()
		if !strings.Contains(msg, "不支持硬链接") {
			t.Fatalf("%s: 没点出卷型不支持：%s", c.name, msg)
		}
		if !strings.Contains(msg, "exFAT") {
			t.Fatalf("%s: 没给出可操作的例子（用户要的是「哪种卷」）：%s", c.name, msg)
		}
		if strings.Contains(msg, "跨卷或权限") || strings.Contains(msg, "权限不足") {
			t.Fatalf("%s: 仍把排查方向指向权限（M273 的原始症状）：%s", c.name, msg)
		}
	}
}

// 跨卷与权限各自有各自的归因，不与"卷型"混成一锅。
func TestM273CrossDeviceAndPermissionKeepSeparateCauses(t *testing.T) {
	cross := hardlinkLinkError(m273LinkErr(winErrorNotSameDevice)).Error()
	if !strings.Contains(cross, "同一卷") || strings.Contains(cross, "不支持硬链接") {
		t.Fatalf("跨卷那一支归因不准：%s", cross)
	}
	denied := hardlinkLinkError(m273LinkErr(syscall.Errno(5))).Error()
	if !strings.Contains(denied, "权限") || strings.Contains(denied, "不支持硬链接") {
		t.Fatalf("ERROR_ACCESS_DENIED 那一支归因不准：%s", denied)
	}
}

// 认不出的码**不许**假装知道：三种可能都要列出来（与 M289/M270 的"三态如实"同族）。
func TestM273UnknownErrnoListsAllCandidates(t *testing.T) {
	msg := hardlinkLinkError(m273LinkErr(syscall.Errno(9999))).Error()
	for _, want := range []string{"卷", "权限"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("未知码 %v 的兜底丢了候选 %q：%s", syscall.Errno(9999), want, msg)
		}
	}
	if strings.Contains(msg, "可随时还原") {
		t.Fatalf("不知从哪抄来的句子：%s", msg)
	}
}

// 原始错误必须仍在（%w 链 + 两个路径），否则失败清单失去可定位性。
func TestM273KeepsWrappedErrorAndPaths(t *testing.T) {
	wrapped := m273LinkErr(syscall.Errno(1))
	err := hardlinkLinkError(wrapped)
	var le *os.LinkError
	if !errors.As(err, &le) {
		t.Fatalf("包装链断了（errors.As 取不到 *os.LinkError）：%v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, `keep.bin`) || !strings.Contains(msg, `dup.bin.fdd-tmp`) {
		t.Fatalf("两个路径没都留在消息里：%s", msg)
	}
}
