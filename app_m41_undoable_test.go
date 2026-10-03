package main

// APP-41（2026-10-03 审查收尾项，I5 形状）：回撤两条腿的可撤判据不得只读账本那一列。
//
// 形状：改前 `UndoOperation` 与 `UndoOperationItem` 各自写 `if !meta.Undoable`，
// 而 `op_records.undoable` 是**记账那一刻**按当时的平台写的
// （`beginJournal` → `undoableFor(kind, runtime.GOOS)`）⇒ "账本说可撤"与
// "本机真能撤"成了两份判据。把一份在 Windows 上写好的账本拿到 macOS 打开
// （DDP-9 落地后外来账本会在导入时被压成 0，但**同机跨平台复制**这一档仍在），
// 那一列就与本机能力不一致 ⇒ 用户点到一个注定失败的动作。
//
// ★ 为什么本条曾被列为"待裁"而不是"必改"：DDP-9 已经封了主路（外来账本导入时
//   压成 0），本条是**纵深防御**——它收掉的是"两份判据"这个 I5 形状本身。
//   代价为零：对同一台机器写下的账本，两列本来就同值，这一句恒真（no-op）。
//
// 判据落在**读侧**（opUndoAllowed），且平台真值经参数注入 ⇒ 纯函数在任何一台机器上
// 都能把三平台的真值表断言掉（沿用 APP-6 那批的手法）。

import (
	"runtime"
	"strings"
	"testing"

	"filededup/internal/history"
)

// P-55 三平台真值表：平台判据与账本那一列**同源**，但只有本机那格能放行。
// 变异用：把 opUndoAllowed 里的 `&& undoableFor(...)` 删掉，本格必红。
func TestAPP41OpUndoAllowedAsksBothColumns(t *testing.T) {
	cases := []struct {
		kind     string
		undoable bool // 账本里那一列
		want     bool
		why      string
	}{
		// 本机（darwin）能撤的：账本说可撤 ⇒ 放行
		{"trash", true, true, "darwin 上 trash 走应用内回收站映射，可撤"},
		{"move", true, true, "move 一律可撤"},
		{"hardlink", true, true, "硬链接拆除可撤"},
		// 账本说不可撤 ⇒ 无论平台都拒（这一列是记账时的判断，必须认）
		{"delete", true, false, "永久删除物理上无从恢复"},
		{"trash", false, false, "账本说不可撤就不可撤（外来账本 DDP-9 压的就是这列）"},
		{"move", false, false, "同上"},
	}
	for _, c := range cases {
		meta := &history.OpMeta{Kind: c.kind, Undoable: c.undoable}
		if got := opUndoAllowed(meta); got != c.want {
			t.Errorf("opUndoAllowed(%s, undoable=%v) = %v，期望 %v（%s）",
				c.kind, c.undoable, got, c.want, c.why)
		}
	}
}

// P-56 平台不一致那一格：账本说可撤、但本机平台不允许 ⇒ 拒。
// 这一格是 APP-41 的**行为判据本体**：它是"两份判据"真正分岔的那一格。
//
// ★ 本机是 darwin，所以 trash 恒可撤 ⇒ 这一格在本机恒真、断言不到。
//
//	按 APP-6 那批立下的规矩（平台真值经参数注入才可测），这里改为断言
//	**纯函数那一侧**：`undoableFor("trash", "windows")` 为假，而
//	`opUndoAllowed` 在**本机**上与 `undoableFor(kind, runtime.GOOS)` 同值
//	—— 后者保证"opUndoAllowed 真的在问平台判据"（变异删掉它本格即红）。
func TestAPP41PlatformColumnIsActuallyConsulted(t *testing.T) {
	if undoableFor("trash", "windows") {
		t.Fatal("前提不成立：undoableFor(\"trash\", \"windows\") 应为假（Windows 回收站无落点映射）")
	}
	if !undoableFor("trash", "darwin") {
		t.Fatal("前提不成立：undoableFor(\"trash\", \"darwin\") 应为真")
	}
	// opUndoAllowed 在本机上必须与"账本列 && 本机平台列"逐字同值。
	// 这一格抓的是"忘了问平台列"（把 && 那半句删掉 ⇒ 本格红）。
	for _, kind := range []string{"trash", "move", "delete", "hardlink", "symlink"} {
		for _, col := range []bool{true, false} {
			meta := &history.OpMeta{Kind: kind, Undoable: col}
			want := col && undoableFor(kind, runtime.GOOS)
			if got := opUndoAllowed(meta); got != want {
				t.Errorf("opUndoAllowed(%s, %v) = %v，而「账本列 && undoableFor(%s, %s)」= %v "+
					"⇒ 平台列没被真的问进去（删掉 && 那半句会红在这里）",
					kind, col, got, kind, runtime.GOOS, want)
			}
		}
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
