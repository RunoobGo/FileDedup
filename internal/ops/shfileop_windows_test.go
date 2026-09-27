//go:build windows

package ops

// M280 的 **Windows 接线腿**：译文表在 `shfileop.go`（无 tag，Linux CI 跑它），
// 本文件只钉一件事——`trash_windows.go` 的错误出口**确实**走了那张表。
//
// 为什么光测表不够：产生点在 `defaultTrashLocked` 里，我把那行改回
// `fmt.Errorf("SHFileOperation 错误码 %d", r0)` 时，表的测试仍然全绿，
// 而用户看到的又是一句裸码（M280 的原始形状）。

import (
	"os"
	"strings"
	"testing"
)

func TestM280WindowsCallSiteUsesTheReasonTable(t *testing.T) {
	raw, err := os.ReadFile("trash_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	i := strings.Index(src, "func defaultTrashLocked(")
	if i < 0 {
		t.Fatal("找不到 defaultTrashLocked ⇒ 锚的扫描面对不上，先重读结构")
	}
	body := src[i:]
	if j := strings.Index(body, "\n// recycleEvidence"); j > 0 {
		body = body[:j]
	}
	if !strings.Contains(body, "shFileOperationError(") {
		t.Fatal("Shell 失败出口没有走译文表 ⇒ M280 的裸码又会直达用户")
	}
	// 反向钉：那句裸码文案不得作为**第二份实现**留在产生处（表只许有一处）。
	if strings.Contains(body, `"SHFileOperation 错误码 %d"`) {
		t.Fatal("产生处仍自带裸码文案 ⇒ 译文表成了摆设")
	}
	if !strings.Contains(body, "M280") {
		t.Fatal("接线处没有了 M280 的锚 ⇒ 下一位读者无从知道这行为什么不能改回裸码")
	}
}
