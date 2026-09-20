package ops

import (
	"strings"
	"testing"

	"filededup/internal/model"
)

// 2026-09-20 全面代码审查（D1）：空组索引越界 panic 的回归测试。
//
// 背景：扫描流水线自身不可能产出空组——internal/dedup/pipeline.go 在输出
// 前有 `if len(g) < 2 { continue }`。但 **Execute 的 Groups 是调用方传入的**，
// 而结果集有两个来源：
//   - 扫描：恒 ≥2 员（受上面的过滤保护）
//   - 历史恢复（history.LoadScan 读 hist_groups / hist_files 两张表）：
//     hist_files 被外部工具删过、或库影像异常时，完全可能交出一个
//     "有 hist_groups 行、无 hist_files 行"的组。
//
// 修正前 executor.go 的 keep 选取写作 `g.Files[pickShortest(g)]`，空组时
// pickShortest 返回 0 → `g.Files[0]` 越界 panic。执行发生在 goroutine 里，
// panic 会带走整批清理的收尾（写前账本停在 planned、结果集不回写）。
//
// 这些用例在修正前**会 panic**，因此是"缺陷必然被拦住"的强形式。

// TestExecuteEmptyGroupDoesNotPanic 空组混在正常组里时，Execute
// 必须照常处理正常组、跳过空组，而不是崩溃。
//
// ★ 这是 D1 的**差分**回归：Exec.Execute 的 keep 选取写作
// `g.Files[pickShortest(g)]`，对空组而言 pickShortest 返回 0 →
// `g.Files[0]` 越界 → 修正前本用例必 panic（不是断言失败，是崩进程）。
func TestExecuteEmptyGroupDoesNotPanic(t *testing.T) {
	fx := newFixture(t)
	empty := &model.DuplicateGroup{GroupID: 99} // 无成员

	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{empty, fx.group},
		KeepIDs: map[uint64]bool{fx.orig.ID: true},
	}, model.OpRequest{
		Kind:          "delete",
		FileIDs:       []uint64{fx.dup1.ID},
		ConfirmDanger: true,
	})

	// 正常组里的 dup1 应被正常处理（delete 走真删）
	if len(res.OK) != 1 || res.OK[0] != fx.dup1.Path {
		t.Fatalf("正常组的文件未被处理：OK=%v Failed=%v", res.OK, res.Failed)
	}
	// 空组不该产生任何"文件不在当前结果集"之类的假失败：它没有成员，
	// 也就没有任何 id 会指向它，Failed 只可能来自形参错误。
	for _, f := range res.Failed {
		if f.Path == "" {
			t.Fatalf("空组在混组场景下不应添加任何无路径失败（形参错误只源于 FileIDs），got %v", f)
		}
	}
}

// TestExecuteOnlyEmptyGroupsReturnsNoFiles 只有空组时的两条真实契约：
//
//	(1) 不得 panic——这是 D1 的修正点；
//	(2) 空组本身**不得**产出任何 item 回调（它没有任何成员文件可处理），
//	    唯一允许出现的失败是"所选 id 找不到归属"这条调用方错误。
//
// 注意这条用例**不**要求"失败必须有路径"：`文件不在当前结果集（id=…）` 的
// Path 天然为空（executor.go:217），它是形参错误而非文件级失败，与空组无关。
// 空组对 Failed 的贡献是 0。
func TestExecuteOnlyEmptyGroupsReturnsNoFiles(t *testing.T) {
	empty := &model.DuplicateGroup{GroupID: 1}

	var items []ItemResult
	res := Execute(Options{
		Groups: []*model.DuplicateGroup{empty},
		OnItem: func(r ItemResult) { items = append(items, r) },
	}, model.OpRequest{Kind: "trash", FileIDs: []uint64{12345}})

	if len(res.OK) != 0 {
		t.Fatalf("空组里不可能有可处理文件，OK 应为空：%v", res.OK)
	}
	if len(items) != 0 {
		t.Fatalf("空组不得产出任何 item 回调，got %v", items)
	}
	// 只为那条"id 无归属"的形参错误记一条失败，不多不少。
	if len(res.Failed) != 1 {
		t.Fatalf("空组场景下 Failed 应恒为 1 条（形参错误），got %v", res.Failed)
	}
	if !containsStr(res.Failed[0].Err, "不在当前结果集") {
		t.Fatalf("唯一那条失败应指出 id 无归属，got %v", res.Failed[0])
	}
}

// TestExecuteEmptyGroupMixedWithKeepIDs 空组不影响 keepIDs 的解析：
// 有决策的组仍按决策选保留者（不因空组存在而退回 pickShortest）。
func TestExecuteEmptyGroupMixedWithKeepIDs(t *testing.T) {
	fx := newFixture(t)
	// 显式把 dup1 标为保留（与默认的 keep 相反）
	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{{GroupID: 7}, fx.group},
		KeepIDs: map[uint64]bool{fx.dup1.ID: true},
	}, model.OpRequest{Kind: "trash", FileIDs: []uint64{fx.dup1.ID}})

	if len(res.Failed) != 1 {
		t.Fatalf("保留项应被拒绝执行，got Failed=%v", res.Failed)
	}
	if !containsStr(res.Failed[0].Err, "保留文件不可操作") {
		t.Fatalf("应命中 S2 保留项拒绝，got %v", res.Failed[0])
	}
}

func containsStr(s, sub string) bool {
	return strings.Contains(s, sub)
}
