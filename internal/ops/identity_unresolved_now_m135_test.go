package ops

// M135（第 3 轮 §24.4，04 §6.11 OPS-17）：identityCheck 的第二条 vUnknown 来路
// 此前**零断言**——M114 用注入"读身份报错"造出了第一条来路（`identity_unknown_m114_test.go`），
// 但 verify.go:144-146 那一条（原先能解析、现在解析不出）改前被变异成
// `return vReplaced, ""` 时整个 ops 包全绿（§24.2 的 R3-MU1）。
//
// 为什么这一格值得单独钉：它是"身份读取成功、但结果说不出身份"，与"读取失败"
// 在用户侧该做的动作不同（前者多半是路径被换成了不提供稳定索引的对象，如换卷、
// 换文件系统；后者是权限/断线），而两者都**不许**说成"看到了顶替"。
// 处置照旧 fail-closed（拦下），本条只钉说法与来路。
//
// 注入点沿用 M114 的 `fsidFromPathFn` 包级接缝：这次返回的不是 error，而是
// `Resolved=false` 的零值 ID——生产里对应"FAT/exFAT 那类不给稳定索引的卷"，
// 无 root 的本机造不出这样的位置，只能从接缝进。

import (
	"os"
	"strings"
	"testing"

	"filededup/internal/fsid"
	"filededup/internal/model"
)

func TestIdentityNowUnresolvableIsNotReportedAsReplaced(t *testing.T) {
	// ---- 夹具前提自检（不经判据本体，I5）----
	if (fsid.ID{}).Resolved {
		t.Fatal("夹具前提不成立：零值 fsid.ID 竟是已解析的，注入打不到 vUnknown 那一格")
	}

	fx := newFixture(t)
	victim := fx.dup1.Path
	var hits int
	prev := fsidFromPathFn
	fsidFromPathFn = func(p string) (fsid.ID, error) {
		if p == victim {
			hits++
			return fsid.ID{}, nil // 读得动，但拿不到稳定身份
		}
		return prev(p)
	}
	t.Cleanup(func() { fsidFromPathFn = prev })

	res := Execute(Options{Groups: []*model.DuplicateGroup{fx.group}, KeepIDs: map[uint64]bool{fx.orig.ID: true}},
		model.OpRequest{Kind: "delete", FileIDs: []uint64{fx.dup1.ID}, ConfirmDanger: true})

	// ---- 前提自检：注入点真的被走到了 ----
	if hits == 0 {
		t.Fatal("前提自检：身份读取从未落到被测路径上 ⇒ 探针打在空处，本条没有取到读数")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("前提自检：文件应还在盘上（注入的是「解析不出身份」，不是「不存在」）: %v", err)
	}

	// ---- 处置不许回退：无从判定仍然拦下 ----
	if len(res.Skipped) != 0 {
		t.Errorf("身份解析不出不得按 Skipped 处置（那是「确实已消失」的口径）: %v", res.Skipped)
	}
	if len(res.Failed) != 1 {
		t.Fatalf("Failed = %+v, want 1 条（拦下是对的，本条只钉说法）", res.Failed)
	}
	msg := res.Failed[0].Err

	// ---- 判据格：不能把"现在解析不出"说成"换成了另一个对象" ----
	if strings.Contains(msg, "被替换") {
		t.Errorf("身份解析不出被说成了确证的顶替（M135）：%q\n"+
			"这一格的来路是 verify.go:144-146：两侧都读得动，只是新读到的身份解析不出，"+
			"没有任何证据表明存在一个「换上来的新文件」", msg)
	}
	if !strings.Contains(msg, "无法确认") {
		t.Errorf("文案应明说「无法确认」，与「读不动」那一格同族: %q", msg)
	}
	if !strings.Contains(msg, "原先能解析身份") {
		t.Errorf("必须带上这一格自己的因由（用户据此区分「换到了不给稳定索引的卷」与「权限/断线」）: %q", msg)
	}
}
