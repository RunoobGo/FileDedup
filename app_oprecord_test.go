package main

// v0.5.0 功能 5（2026-09-20）：软链接合并的执行反馈与历史标注。

import (
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/history"
	"filededup/internal/ops"
	"filededup/internal/worktemp"
)

// buildDanglingOpRecord 在真实历史库中登记一笔软链接操作，并手工构造三种条目状态：
//
//	done  + 有效链接   → 应标 IsSymlink=true, Dangling=false
//	done  + 悬空链接   → 应标 IsSymlink=true, Dangling=true
//	done  + 普通文件   → 应标 IsSymlink=false（不是链接，不该标）
//	failed             → **不得**被检测（原位是什么都不影响渲染）
//
// 直接用 BeginOp/FinishItem 落库，而不是跑一遍 ExecuteOperation：
// 后者要求文件真的存在于结果集中且能通过内容校验，构造"执行成功但目标随后
// 被删"的时序要绕很大一圈；而本用例要验证的是**读取侧的标注逻辑**，
// 直接构造终态更贴近它的职责边界。
func buildDanglingOpRecord(t *testing.T, a *App) (int64, string, string) {
	t.Helper()
	dir := t.TempDir()

	keep := filepath.Join(dir, "keep.bin")
	if err := os.WriteFile(keep, []byte("payload-dup-content"), 0o644); err != nil {
		t.Fatal(err)
	}

	good := filepath.Join(dir, "good.bin")
	dead := filepath.Join(dir, "dead.bin")
	plain := filepath.Join(dir, "plain.bin")

	// 通过 ops 的公开能力建链接太绕（SymlinkMerge 会改名备份），
	// 这里直接用 os.Symlink —— 但要先探明环境支持。
	if err := os.Symlink(keep, good); err != nil {
		t.Skipf("本环境不支持创建符号链接（%v），无法构造前置状态", err)
	}
	if err := os.Symlink(filepath.Join(dir, "vanished.bin"), dead); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plain, []byte("plain"), 0o644); err != nil {
		t.Fatal(err)
	}

	plans := []history.OpItemPlan{
		{OrigPath: good, Size: 18},
		{OrigPath: dead, Size: 18},
		{OrigPath: plain, Size: 5},
		{OrigPath: filepath.Join(dir, "never.bin"), Size: 1},
	}
	opID, err := a.hist.BeginOp("symlink", "", 0, true, plans)
	if err != nil {
		t.Fatal(err)
	}
	finish := func(p, linkSrc, state string) {
		if err := a.hist.FinishItem(opID, p, "", linkSrc, state, ""); err != nil {
			t.Fatal(err)
		}
	}
	finish(good, keep, history.StateDone)
	finish(dead, filepath.Join(dir, "vanished.bin"), history.StateDone)
	finish(plain, keep, history.StateDone)
	finish(filepath.Join(dir, "never.bin"), keep, history.StateFailed)
	return opID, good, dead
}

// TestGetOpRecordMarksSymlinkAndDangling GetOpRecord 必须逐条标注链接状态，
// 且只标注 state=done 的条目。
//
// 这是历史记录页「链接已失效」红标的**唯一**数据来源。若这里搞错：
//   - 漏标 → 用户看不到链接已失效，直到某天发现文件打不开；
//   - 多标（给 failed/skipped 条目检测）→ 那些条目本就没在文件系统上动过手，
//     原位可能是用户自己的文件，标红纯属误导。
func TestGetOpRecordMarksSymlinkAndDangling(t *testing.T) {
	a, _ := newHistApp(t)
	opID, good, dead := buildDanglingOpRecord(t, a)

	d, err := a.GetOpRecord(opID)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]OpRecordItem{}
	for _, it := range d.Items {
		byPath[it.OrigPath] = it
	}
	if len(d.Items) != 4 {
		t.Fatalf("条目数应为 4，实得 %d", len(d.Items))
	}

	// ① 有效链接
	if it := byPath[good]; !it.IsSymlink || it.Dangling {
		t.Fatalf("有效链接应标 (isSymlink=true, dangling=false): %+v", it)
	}
	// ② 悬空链接 —— 本用例的核心断言
	if it := byPath[dead]; !it.IsSymlink || !it.Dangling {
		t.Fatalf("❌ 悬空链接必须被识别（历史页据此标红提示用户）: %+v", it)
	}
	// ③ 普通文件不得被标为链接
	plainPath := filepath.Join(filepath.Dir(good), "plain.bin")
	if it := byPath[plainPath]; it.IsSymlink || it.Dangling {
		t.Fatalf("普通文件不应被标为链接: %+v", it)
	}
	// ④ failed 条目不得被检测（原位不存在，且它本就没动过文件系统）
	never := filepath.Join(filepath.Dir(good), "never.bin")
	if it := byPath[never]; it.IsSymlink || it.Dangling {
		t.Fatalf("非 done 条目不应被标注（原位可能有意料之外的文件，标红是误导）: %+v", it)
	}
}

// TestGetOpRecordLinkSrcPreserved LinkSrc 必须原样透出——前端的
// 「软链接 → 目标」文案与悬空说明都依赖它。
func TestGetOpRecordLinkSrcPreserved(t *testing.T) {
	a, _ := newHistApp(t)
	opID, good, _ := buildDanglingOpRecord(t, a)

	d, err := a.GetOpRecord(opID)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range d.Items {
		if it.OrigPath != good {
			continue
		}
		want := filepath.Join(filepath.Dir(good), "keep.bin")
		if it.LinkSrc != want {
			t.Fatalf("LinkSrc 应原样透出（前端据此渲染「软链接 → 目标」）: 期望 %q，实得 %q",
				want, it.LinkSrc)
		}
		return
	}
	t.Fatal("未找到目标条目")
}

// TestGetOpRecordAfterUndoNotMarked 回撤之后（链接已被删除）不得报错，
// 也不得残留"链接"标记——这是最正常的后续状态。
func TestGetOpRecordAfterUndoNotMarked(t *testing.T) {
	a, _ := newHistApp(t)
	opID, good, _ := buildDanglingOpRecord(t, a)

	// 模拟回撤：链接被删
	if err := os.Remove(good); err != nil {
		t.Fatal(err)
	}
	d, err := a.GetOpRecord(opID)
	if err != nil {
		t.Fatalf("回撤后读取明细不得报错: %v", err)
	}
	for _, it := range d.Items {
		if it.OrigPath == good && (it.IsSymlink || it.Dangling) {
			t.Fatalf("链接已删除，不应再标为链接: %+v", it)
		}
	}
}

// TestWorkTempNamesNotMistakenForLinks 回归（缺陷 6 的关联面）：
// 合并过程的临时名（.fdd-tmp / .fdd-old）必须被 worktemp 识别，
// 否则它们会被当成"链接"或"重复文件"进入历史/扫描。
func TestWorkTempNamesNotMistakenForLinks(t *testing.T) {
	for _, suffix := range []string{ops.FddTempSuffix, ops.FddOldSuffix} {
		p := "/data/photo.jpg" + suffix
		if !worktemp.IsTempName(filepath.Base(p)) {
			t.Fatalf("%s 应被识别为工作临时文件（否则会成为幽灵重复项）", p)
		}
	}
}
