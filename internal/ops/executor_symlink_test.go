package ops

// ============================================================================
// 执行器接入：软链接合并（2026-09-20）
//
// 覆盖执行器的四个关键行为：
//   1. 白名单接纳 symlink（未被判为"未知操作类型"）
//   2. 成功路径：dup 变成指向 keep 的链接，且**不**计入 Reclaimed
//   3. 同卷时给出非阻断 Warnings（不判失败）
//   4. 权限不足时判失败，但错误指向"权限"而非"环境故障"
//
// 同时验证：软链接合并同样受 S1（篡改拦截）与 S2（保留项保护）约束——
// 这两条是安全语义，不能因为换了链接类型就放松。
// ============================================================================

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/fsid"
	"filededup/internal/model"
)

// symlinkFixture 造一个三文件重复组，并探测环境是否支持软链接。
func symlinkFixture(t *testing.T) (*fixture, func()) {
	t.Helper()
	fx := newFixture(t)
	requireSymlinkSupport(t, fx.dir)
	return fx, func() {}
}

// TestExecuteSymlinkMergesDupIntoLink 成功路径。
func TestExecuteSymlinkMergesDupIntoLink(t *testing.T) {
	fx := newFixture(t)
	requireSymlinkSupport(t, fx.dir)

	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		KeepIDs: map[uint64]bool{fx.orig.ID: true},
	}, model.OpRequest{Kind: "symlink", FileIDs: []uint64{fx.dup2.ID}})

	if len(res.OK) != 1 || res.OK[0] != fx.dup2.Path {
		t.Fatalf("软链接合并应成功: %+v", res)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("不应有失败项: %+v", res.Failed)
	}
	assertSymlinkCriterion(t, fx.orig.Path, fx.dup2.Path)
	assertNoSymlinkResidue(t, fx.dup2.Path)

	// 会计口径：软链接不并入 Reclaimed，单列 SymlinkedBytes
	if res.Reclaimed != 0 {
		t.Fatalf("软链接合并不应计入 Reclaimed（须单列以便前端表述悬空风险）: %d", res.Reclaimed)
	}
	if res.SymlinkedBytes != fx.dup2.Size {
		t.Fatalf("SymlinkedBytes 应为 %d，实得 %d", fx.dup2.Size, res.SymlinkedBytes)
	}
	if res.LinkedBytes != 0 {
		t.Fatalf("软链接不应计入 LinkedBytes（那是硬链接的栏位）: %d", res.LinkedBytes)
	}
}

// TestExecuteSymlinkWarnsOnSameVolume 同卷时只提示不阻断。
//
// 判据：操作必须成功（OK 里有它），且 Warnings 里出现"硬链接"字样。
// 若某天有人把同卷做成硬性拒绝，本用例会立刻失败——那正是要防的回归。
func TestExecuteSymlinkWarnsOnSameVolume(t *testing.T) {
	fx := newFixture(t)
	requireSymlinkSupport(t, fx.dir)

	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		KeepIDs: map[uint64]bool{fx.orig.ID: true},
	}, model.OpRequest{Kind: "symlink", FileIDs: []uint64{fx.dup2.ID}})

	if len(res.OK) != 1 {
		t.Fatalf("同卷使用软链接应**成功**（仅提示，不阻断）: %+v", res)
	}
	// fixture 的三个文件都在同一个 t.TempDir() 里 → 必然同卷
	found := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "硬链接") {
			found = true
			t.Logf("同卷提示: %s", w)
		}
	}
	if !found {
		t.Fatalf("同卷使用软链接时应提示改用硬链接，实得 Warnings=%v", res.Warnings)
	}
}

// TestExecuteSymlinkPrivilegeFailureIsIsolated 建链接失败（权限）时：
// 该文件判失败、其它文件不受影响、原文件完好。
func TestExecuteSymlinkPrivilegeFailureIsIsolated(t *testing.T) {
	fx := newFixture(t)
	// 不探测环境支持——本用例用注入点模拟失败，任何平台都能跑

	origHook := symlinkCreateFn
	defer func() { symlinkCreateFn = origHook }()
	symlinkCreateFn = func(string, string) error { return ErrSymlinkNeedsPrivilege }

	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		KeepIDs: map[uint64]bool{fx.orig.ID: true},
	}, model.OpRequest{Kind: "symlink", FileIDs: []uint64{fx.dup1.ID, fx.dup2.ID}})

	if len(res.OK) != 0 {
		t.Fatalf("建链接失败时不应有成功项: %+v", res)
	}
	if len(res.Failed) != 2 {
		t.Fatalf("两个文件都应记为失败: %+v", res.Failed)
	}
	for _, f := range res.Failed {
		if f.Stage != "symlink-privilege" {
			t.Fatalf("失败阶段应为 symlink-privilege（供上层汇总权限指引）: %+v", f)
		}
	}
	// 原文件必须完好无损
	assertDupIsPlainFileWithContent(t, fx.dup1.Path, string(mustRead(t, fx.orig.Path)))
	assertDupIsPlainFileWithContent(t, fx.dup2.Path, string(mustRead(t, fx.orig.Path)))
}

// TestExecuteSymlinkRespectsKeepProtection S2：保留项不得被操作。
func TestExecuteSymlinkRespectsKeepProtection(t *testing.T) {
	fx := newFixture(t)
	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		KeepIDs: map[uint64]bool{fx.orig.ID: true},
	}, model.OpRequest{Kind: "symlink", FileIDs: []uint64{fx.orig.ID}})

	if len(res.OK) != 0 {
		t.Fatalf("保留项不应被执行: %+v", res)
	}
	if len(res.Failed) != 1 || !strings.Contains(res.Failed[0].Err, "保留") {
		t.Fatalf("应报「保留文件不可操作」: %+v", res.Failed)
	}
	// 保留项必须仍是普通文件
	li, _ := os.Lstat(fx.orig.Path)
	if li.Mode()&os.ModeSymlink != 0 {
		t.Fatal("保留项不应被替换为链接")
	}
}

// TestExecuteSymlinkBlocksTamperedDup S1：dup 被篡改后不得合并。
func TestExecuteSymlinkBlocksTamperedDup(t *testing.T) {
	fx := newFixture(t)
	requireSymlinkSupport(t, fx.dir)

	raw, err := os.ReadFile(fx.dup2.Path)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)/2] ^= 0xFF
	if err := os.WriteFile(fx.dup2.Path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		KeepIDs: map[uint64]bool{fx.orig.ID: true},
	}, model.OpRequest{Kind: "symlink", FileIDs: []uint64{fx.dup2.ID}})

	if len(res.OK) != 0 {
		t.Fatalf("被篡改的 dup 不应合并: %+v", res)
	}
	if len(res.Failed) != 1 || res.Failed[0].Stage != "verify" {
		t.Fatalf("应在 verify 阶段拦截: %+v", res.Failed)
	}
	// 文件必须还在原位、未被替换成链接
	li, err := os.Lstat(fx.dup2.Path)
	if err != nil {
		t.Fatalf("被拦截的文件不应消失: %v", err)
	}
	if li.Mode()&os.ModeSymlink != 0 {
		t.Fatal("被拦截的文件不应被替换成链接")
	}
}

// TestExecuteSymlinkBlocksTamperedKeep S1 扩展：保留源被篡改后不得合并。
//
// 与硬链接同理：源内容变了还去建链接，用户以为"链接指向原来那份"，
// 实际读到的却是被改过的数据——不可逆的误导。
func TestExecuteSymlinkBlocksTamperedKeep(t *testing.T) {
	fx := newFixture(t)
	requireSymlinkSupport(t, fx.dir)

	raw, err := os.ReadFile(fx.orig.Path)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)/2] ^= 0xFF
	if err := os.WriteFile(fx.orig.Path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		KeepIDs: map[uint64]bool{fx.orig.ID: true},
	}, model.OpRequest{Kind: "symlink", FileIDs: []uint64{fx.dup2.ID}})

	if len(res.OK) != 0 {
		t.Fatalf("保留源被篡改时不应合并: %+v", res)
	}
	if len(res.Failed) != 1 || res.Failed[0].Stage != "verify" {
		t.Fatalf("应在 verify 阶段拦截保留源: %+v", res.Failed)
	}
	// dup 必须仍是原内容（未被改动、未被替换成链接）
	assertDupIsPlainFileWithContent(t, fx.dup2.Path, string(mustRead(t, fx.dup1.Path)))
}

// TestExecuteSymlinkUnknownKindStillRejected 白名单仍须拦住真正的未知类型
// （确保加 "symlink" 不是把校验放宽成"什么都收"）。
func TestExecuteSymlinkUnknownKindStillRejected(t *testing.T) {
	fx := newFixture(t)
	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		KeepIDs: map[uint64]bool{fx.orig.ID: true},
	}, model.OpRequest{Kind: "symlinkz", FileIDs: []uint64{fx.dup2.ID}})

	if len(res.Failed) != 1 || !strings.Contains(res.Failed[0].Err, "未知操作类型") {
		t.Fatalf("拼错的类型名必须被拒: %+v", res.Failed)
	}
}

// TestExecuteSymlinkUsesRealIdentityWhenAvailable 在身份可解析的环境里，
// 带真实 ID 的整条链路（执行器 → SymlinkMerge）必须成功。
//
// 这是 §3.4 那个坑的**端到端**守卫：若 SymlinkMerge 步骤 2 误用
// identityStill(tmp, keepID)，执行器传入的真实 keepID 会让它必然失败。
func TestExecuteSymlinkUsesRealIdentityWhenAvailable(t *testing.T) {
	fx := newFixture(t)
	requireSymlinkSupport(t, fx.dir)

	kid, err := fsid.FromPath(fx.orig.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !kid.Resolved {
		t.Skipf("本平台/卷不提供稳定文件身份，本用例无实际约束力")
	}

	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		KeepIDs: map[uint64]bool{fx.orig.ID: true},
	}, model.OpRequest{Kind: "symlink", FileIDs: []uint64{fx.dup2.ID}})

	if len(res.OK) != 1 {
		t.Fatalf("❌ 身份可解析环境下带真实 ID 的端到端合并必须成功"+
			"（失败通常意味着 SymlinkMerge 步骤 2 误用了 identityStill）: %+v", res)
	}
	assertSymlinkCriterion(t, fx.orig.Path, fx.dup2.Path)
}

// -- 小工具 --

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// 编译期确认测试目录布局无误（避免 unused import）。
var _ = filepath.Join

// TestAggregateWarningsCollapsesPrivilegeFailures 环境级失败归一化：
// N 个文件因同一权限原因失败时，条目数不变（计数是事实），
// 但完整指引只出现一条，条目级文案被折叠成短句。
//
// 为什么这是必要的（而不是"文案优化"）：真机上一次 200 个文件的跨卷合并，
// 未提权时会产出 200 条各数百字的相同指引——结果页无法阅读，
// 用户反而看不出"到底哪里错了、该怎么办"。
func TestAggregateWarningsCollapsesPrivilegeFailures(t *testing.T) {
	res := model.OpsResult{
		Failed: []model.FailedItem{
			{Path: `D:\a.bin`, Stage: symlinkPrivilegeStage, Err: "很长很长的原始 Windows 报错文案"},
			{Path: `D:\b.bin`, Stage: symlinkPrivilegeStage, Err: "很长很长的原始 Windows 报错文案"},
			{Path: `D:\c.bin`, Stage: symlinkPrivilegeStage, Err: "很长很长的原始 Windows 报错文案"},
			{Path: `D:\d.bin`, Stage: "verify", Err: "文件在扫描后被修改"},
		},
	}
	AggregateWarnings(&res)

	if len(res.Failed) != 4 {
		t.Fatalf("归一化不得改变失败条数（计数是事实）：%d", len(res.Failed))
	}
	for i := 0; i < 3; i++ {
		if strings.Contains(res.Failed[i].Err, "很长很长的原始") {
			t.Fatalf("第 %d 条应被折叠为短句，实得 %q", i, res.Failed[i].Err)
		}
		if !strings.Contains(res.Failed[i].Err, "权限") {
			t.Fatalf("第 %d 条短句应仍能表明原因，实得 %q", i, res.Failed[i].Err)
		}
	}
	// 非权限失败必须原样保留（原因各不相同，合并会丢信息）
	if res.Failed[3].Stage != "verify" || res.Failed[3].Err != "文件在扫描后被修改" {
		t.Fatalf("非权限失败不应被改动：%+v", res.Failed[3])
	}
	// 汇总恰好一条，且给出三个可选处置
	if len(res.Warnings) != 1 {
		t.Fatalf("应恰好汇总一条指引，实得 %d 条: %v", len(res.Warnings), res.Warnings)
	}
	w := res.Warnings[0]
	for _, want := range []string{"管理员身份运行", "开发者模式", "硬链接合并", "本次 3 个文件"} {
		if !strings.Contains(w, want) {
			t.Fatalf("汇总指引应含 %q，实得：%s", want, w)
		}
	}
}

// TestAggregateWarningsNoPrivilegeFailureNoWarn 无权限失败时不得凭空加提示
// （否则每次成功的软链接合并都会带一条无关警告）。
func TestAggregateWarningsNoPrivilegeFailureNoWarn(t *testing.T) {
	res := model.OpsResult{
		Failed: []model.FailedItem{{Path: "x", Stage: "verify", Err: "被篡改"}},
	}
	AggregateWarnings(&res)
	if len(res.Warnings) != 0 {
		t.Fatalf("不该有汇总提示: %v", res.Warnings)
	}
	// nil 安全性：上层可能对空结果调用
	AggregateWarnings(nil)
}

// TestExecuteSymlinkAggregatesPrivilegeGuidance 端到端：两个文件因权限失败时，
// 执行器返回的 Warnings 里应恰好有一条完整指引（供 UI 顶部展示）。
func TestExecuteSymlinkAggregatesPrivilegeGuidance(t *testing.T) {
	fx := newFixture(t)

	origHook := symlinkCreateFn
	defer func() { symlinkCreateFn = origHook }()
	symlinkCreateFn = func(string, string) error { return ErrSymlinkNeedsPrivilege }

	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		KeepIDs: map[uint64]bool{fx.orig.ID: true},
	}, model.OpRequest{Kind: "symlink", FileIDs: []uint64{fx.dup1.ID, fx.dup2.ID}})

	if len(res.Failed) != 2 {
		t.Fatalf("两个文件都应记为失败: %+v", res.Failed)
	}
	if len(res.Warnings) != 1 {
		t.Fatalf("应汇总为一条权限指引，实得 %d: %v", len(res.Warnings), res.Warnings)
	}
	if !strings.Contains(res.Warnings[0], "本次 2 个文件") {
		t.Fatalf("汇总应点明受影响文件数: %s", res.Warnings[0])
	}
}
