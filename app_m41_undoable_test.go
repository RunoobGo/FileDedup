package main

// APP-41（2026-10-03 审查收尾项）：回撤两条腿的可撤判据必须**只有一处实现**，
// 且那一处**只读账本那一列**。
//
// 形状：改前 `UndoOperation` 与 `UndoOperationItem` 各自写 `if !meta.Undoable` ——
// 同一判据两份内联写法（I5）。本条把它收进 `opUndoAllowed`。
//
// ★★ **本条在实施时改过一次取向，而改的理由是本文件最值钱的东西**：
//
//	第一版是 `meta.Undoable && undoableFor(meta.Kind, runtime.GOOS)` ——
//	在读侧**重问平台**，本意是"账本说可撤"与"本机真能撤"对齐。
//	CI run #99 的 **Windows 腿 16 个用例集体红**，全部指向 `undo-code-windows-trash`。
//
//	根因：`op_records.undoable` 那一列是**记账那一刻**的判断，而它**不只由平台决定** ——
//	`BeginOp` 的 `undoable` 是**入参**（`internal/history/oplog.go`），
//	任何直接写库的调用方（夹具、fdd-cli、将来的导入回填）都能给出与本机平台无关的值。
//	读侧重问 `runtime.GOOS` 把「本机能不能撤」当成了「这笔账能不能撤」；
//	而 Windows 上 trash 恒不可撤（回收站没有落点映射）⇒ Windows 上**所有
//	trash 回撤用例**一律被自己刚写的判据拒掉。
//
//	⇒ 取向：**账本那一列是权威，读侧不再重问平台**。它已经承载了平台判断
//	  （`beginJournal` 写入侧那一次），再问一遍是把同一件事算两次，
//	  而第二次算的是「本机」不是「这笔账」—— 恰好在跨平台复制账本时给出错误答案。
//	  `undoableFor` 留在写入侧与原因分流侧，仍是全包唯一实现。
//
// 这一格是**三平台里只有本机那条腿能量**的判据（`runtime.GOOS`），
// 所以判据本体必须**不依赖平台** —— 这样任何一台机器都能把整张表断言掉。

import (
	"strings"
	"testing"

	"filededup/internal/history"
)

// P-55 判据本体：**只读账本那一列**，逐格覆盖 truth table。
// 这一格在三平台上都应绿（判据不依赖平台）—— 第一版依赖 `runtime.GOOS`，
// 于是 Windows 腿整体红，详见文件头。
func TestAPP41OpUndoAllowedReadsLedgerColumnOnly(t *testing.T) {
	cases := []struct {
		kind     string
		undoable bool
		want     bool
		why      string
	}{
		// 账本说可撤 ⇒ 放行。★ trash 在 Windows 上**也**放行 ——
		// 因为判据不重问平台（本机能不能撤是写入侧 beginJournal 的事）。
		{"trash", true, true, "账本说可撤就放行（平台判断是写入侧的事）"},
		{"move", true, true, "同上"},
		{"hardlink", true, true, "同上"},
		{"symlink", true, true, "同上"},
		// 账本说不可撤 ⇒ 拒（DDP-9 的落点闸压的就是这一列）
		{"trash", false, false, "账本说不可撤就不可撤（外来账本 DDP-9 压的就是这列）"},
		{"move", false, false, "同上"},
		// ★ 不放 "delete + undoable=true" 这一格：判据只读那一列，
		//   delete 也会照放行。而这不是缺陷 —— 写入侧 beginJournal 调的是
		//   undoableFor("delete", …) 恒假，永不会写下这种组合；
		//   读侧再补一条 kind 判据就是把同一件事算第二次，正是本条要收掉的 I5。
	}
	for _, c := range cases {
		meta := &history.OpMeta{Kind: c.kind, Undoable: c.undoable}
		if got := opUndoAllowed(meta); got != c.want {
			t.Errorf("opUndoAllowed(%s, undoable=%v) = %v，期望 %v（%s）",
				c.kind, c.undoable, got, c.want, c.why)
		}
	}
}

// P-56 **平台无关性**：这一格是 APP-41 的核心判据，也是第一版翻车的地方。
//
// 变异用：把 opUndoAllowed 改回 `meta.Undoable && undoableFor(meta.Kind, runtime.GOOS)`
// ⇒ 本格在 **Windows 上必红**（`undoableFor("trash","windows")` 为假），
// 而在 darwin/linux 上仍绿 ⇒ **本地全绿、三腿里只有 Windows 红**，正是 run #99 的形状。
//
// ★ 这就是本条最要紧的一课：**凡在读侧引入 `runtime.GOOS` 的判据，
//
//	必须有一条"判据不依赖平台"的判据盯着**，否则本机绿 ≠ 三平台绿。
func TestAPP41PredicateIsPlatformIndependent(t *testing.T) {
	// 前提：trash 在 Windows 上确实不可撤（回收站无落点映射）——
	// 这条正是 Windows 腿 16 红的那半边，写成显式前提，将来口径变了本格会先提醒。
	if undoableFor("trash", "windows") {
		t.Fatal("前提不成立：undoableFor(\"trash\", \"windows\") 应为**假**（回收站无落点映射）" +
			"—— 若它变成真，说明平台判据本身改了，本组对「读侧不该重问平台」的论证要重做")
	}
	if !undoableFor("trash", "darwin") {
		t.Fatal("前提不成立：undoableFor(\"trash\", \"darwin\") 应为**真**")
	}
	// 判据本身：同一笔账（trash + undoable=true）在**任何**平台判据下都放行。
	// 这与 undoableFor 的平台差异**故意相反** —— 那一列已经是写入侧在本机问过一遍的结果。
	meta := &history.OpMeta{Kind: "trash", Undoable: true}
	if !opUndoAllowed(meta) {
		t.Errorf("opUndoAllowed(trash, undoable=true) = false ⇒ 判据重问了平台，" +
			"于是 Windows 上所有 trash 回撤都会被拒（本机 darwin 上仍绿，" +
			"本地全绿而三腿里只有 Windows 红 —— CI run #99 的形状）")
	}
	// 显式对照：写入侧的平台判据确实在区分平台，两者语义**不同**不是 bug。
	if undoableFor("trash", "windows") == undoableFor("trash", "darwin") {
		t.Error("前提已变：undoableFor 不再区分 windows/darwin 的 trash —— " +
			"本组对「读侧与写入侧语义不同」的对照失去意义")
	}
}

// P-57 静态锚：两条腿都必须调 opUndoAllowed，不许任何一处再写裸 `!meta.Undoable`。
//
// ★ 为什么需要这一格：P-55/P-56 验的是**判据本体的行为**，
//
//	而"两条腿都调了它"这件事它们验不到 —— 有人把其中一条腿改回
//	`if !meta.Undoable` 的话，两条判据又变成两份，而上面两格照样绿。
//	这就是 M93「恒绿却什么都没测」的同形，故必须有静态锚点名两条腿。
func TestAPP41BothUndoLegsCallTheSharedPredicate(t *testing.T) {
	src := readSource(t, "app_ops.go")
	for _, sig := range []string{
		"func (a *App) UndoOperation(opLogID int64) (string, error) {",
		"func (a *App) UndoOperationItem(opLogID, itemID int64) (string, error) {",
	} {
		i := strings.Index(src, sig)
		if i < 0 {
			t.Fatalf("APP-41 静态锚读不到 %s —— 入口签名变了，本锚失效不得读作通过", sig)
		}
		rest := src[i:]
		if !strings.Contains(rest, "if !opUndoAllowed(meta) {") {
			t.Errorf("APP-41：%s 没有调共用的 opUndoAllowed ⇒ 两条腿又变成两份判据（I5 回归）", sig)
		}
		if strings.Contains(rest, "if !meta.Undoable {") {
			t.Errorf("APP-41：%s 里仍有裸 `if !meta.Undoable` ⇒ 绕过了平台判据", sig)
		}
	}
	// 判据本体必须只有一处（两处实现就是 I5 本身）。
	if n := strings.Count(src, "func opUndoAllowed("); n != 1 {
		t.Errorf("APP-41：opUndoAllowed 的定义出现 %d 次，应恰好 1 次（两份实现即 I5）", n)
	}
}
