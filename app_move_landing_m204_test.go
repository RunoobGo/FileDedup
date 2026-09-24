package main

// M204（2026-09-24 裁定「rename 前按实际落点复审 withinDir」，设计稿
// `2026-09-24-move-landing-recheck-m204-design` §3-P-1）：
//
// 入口那一次 `moveTargetAllowed` 与真正的 rename 之间隔着**整个派发窗口**
// （逐文件校验 + 串行搬移）。窗口内目标目录被第三方换成指向别处的链接时，
// `moveFileDetailed` 的 MkdirAll+claimDst+rename 会顺着那条链接写到用户
// 从未授权的位置——执行器只复核**被搬的文件**的身份，从不复问落点归属。
//
// ★ 本文件只用**改前就存在**的公开面（`opsExecuteFn` 接缝 + `ExecuteOperation`），
// 所以它是本批唯一一条**真改前红**：改前树上三格全红（文件落到 outside、源没了、
// 账记 done）。攻击时刻选在 `opsExecuteFn` 里，正落在"入口校验已过、执行器未动手"
// 那一格，与真实第三方的时序同形，不靠 sleep 赌。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/model"
	"filededup/internal/ops"
)

func TestM204MoveLandingRecheckedBeforeRename(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	payload := []byte("M204-LANDING-RECHECK-PAYLOAD")
	src := filepath.Join(root, "dup.bin")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "keep.bin"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	// 合法目标：授权根之内的真实子目录——入口校验放行它，攻击发生在放行之后。
	target := filepath.Join(root, "sub")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	a, rec := newHistApp(t)
	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}, Threads: 2}); err != nil {
		t.Fatal(err)
	}
	if ev := rec.waitTerminal(t, "scan"); ev != "scan:done" {
		t.Fatalf("scan 终止事件 = %s", ev)
	}
	a.authorizeDir(root)
	sel := redundantIDs(t, a)
	if len(sel) != 1 {
		t.Fatalf("应有 1 个冗余项, got %v", sel)
	}

	// 派发窗口内的第三方：入口校验之后、执行器动手之前，把目标目录换成指向外部的链接。
	prev := opsExecuteFn
	swapped := false
	opsExecuteFn = func(o ops.Options, op model.OpRequest) model.OpsResult {
		if err := os.Remove(target); err != nil {
			t.Errorf("摘掉目标目录失败，攻击未成立: %v", err)
		}
		if err := os.Symlink(outside, target); err != nil {
			t.Errorf("平台不支持符号链接，无法构造前置状态: %v", err)
		}
		swapped = true
		return prev(o, op)
	}
	t.Cleanup(func() { opsExecuteFn = prev })

	if _, err := a.ExecuteOperation(model.OpRequest{Kind: "move", FileIDs: sel, TargetDir: target}); err != nil {
		t.Fatal(err)
	}
	waitOpsDone(t, rec)
	if !swapped {
		t.Fatal("接缝没被走到：本用例的前置状态未成立，断言全是空转")
	}

	metas, err := a.hist.ListOps()
	if err != nil || len(metas) != 1 {
		t.Fatalf("操作记录 = %+v err=%v", metas, err)
	}
	_, items, err := a.hist.GetOp(metas[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("条目 = %+v", items)
	}
	// 格①：这一项必须记 failed，且文案说明是**授权落点**的问题（不是笼统的"移动失败"）。
	if items[0].State != "failed" {
		t.Errorf("落点已在执行期逃出授权树，条目却记成 %q（期望 failed）：item=%+v", items[0].State, items[0])
	}
	if !strings.Contains(items[0].Err, "授权") {
		t.Errorf("失败文案必须点名「授权」，用户才知道是这道闸拦的：got %q", items[0].Err)
	}
	// 格②：源文件必须还在原地——拒绝不能以搬走为代价。用账本记的 OrigPath，
	// 不写死文件名（哪个是冗余项由扫描序决定，写死会让断言打到保留锚点上）。
	if _, err := os.Stat(items[0].OrigPath); err != nil {
		t.Errorf("源文件 %s 在落点复审不过时被搬走了（%v）：越权写入不该发生", items[0].OrigPath, err)
	}
	// 格③：未授权目录里不得多出任何东西（含零字节占位）。
	ents, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 {
		names := make([]string, 0, len(ents))
		for _, e := range ents {
			names = append(names, e.Name())
		}
		t.Errorf("越权落点 %s 里多出 %d 项（%s）：一个字节都不该写到授权树之外",
			outside, len(ents), strings.Join(names, ", "))
	}
}
