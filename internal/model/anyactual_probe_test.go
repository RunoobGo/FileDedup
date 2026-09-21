package model

// MODEL-1（2026-09-21 全量审查，设计稿 §15.0-A）。
//
// DuplicateGroup.AnyActualKnown 是 ReclaimActual 那个数字的"已统计"标志（调用点
// app.go 的 ResultView 载荷与 cmd/fdd-cli 的 JSON）。两者的**总体必须一致**：
// ReclaimActual 只加冗余成员（files[1:]，files[0] 是保留项），而 AnyActualKnown
// 原先遍历全部成员 ⇒ 跨卷/部分读数时"只有保留项读到过实占"这一形状会让标志为真，
// 而界面上显示的数字纯是逻辑口径的回退值——这正是 M6-P2 立"两口径并存"要防的互相冒充。

import "testing"

func entry(size, actual uint64, known bool) *FileEntry {
	return &FileEntry{Size: size, Actual: actual, ActualKnown: known}
}

// 主探针：只有保留项有实占读数时，"已统计"标志必须为 false。
func TestAnyActualKnownCoversOnlyRedundantMembers(t *testing.T) {
	g := &DuplicateGroup{Files: []*FileEntry{
		entry(4096, 16384, true), // files[0]：保留项，不参与可释放量
		entry(4096, 0, false),    // 冗余项：本卷没读到实占
	}}
	if g.AnyActualKnown() {
		t.Fatalf("AnyActualKnown=true 而 ReclaimActual=%d 纯是逻辑口径回退值（保留项 files[0] 不该参与这个标志）："+
			"标志与数字来自不同总体 ⇒ 界面把回退值显示成已统计（§15.0-A MODEL-1）",
			ReclaimActual(g.Files))
	}
}

// 负控制一：冗余项读到实占 ⇒ 标志必须为真。
func TestAnyActualKnownTrueWhenRedundantMemberKnown(t *testing.T) {
	g := &DuplicateGroup{Files: []*FileEntry{
		entry(4096, 0, false),
		entry(4096, 4096, true),
	}}
	if !g.AnyActualKnown() {
		t.Fatal("冗余项已读到实占却报 false：会把真数字显示成「未统计」")
	}
	if got := ReclaimActual(g.Files); got != 4096 {
		t.Fatalf("ReclaimActual=%d, want 4096", got)
	}
}

// 负控制二：保留项与冗余项都有读数 ⇒ 数字只加冗余项（M-P2-c 同一条约定）。
func TestAnyActualKnownBothKnownKeepsReclaimWithoutKept(t *testing.T) {
	g := &DuplicateGroup{Files: []*FileEntry{
		entry(4096, 16384, true),
		entry(4096, 8192, true),
	}}
	if !g.AnyActualKnown() {
		t.Fatal("两侧都有读数却报 false")
	}
	if got := ReclaimActual(g.Files); got != 8192 {
		t.Fatalf("ReclaimActual=%d, want 8192（不得把保留项的 16384 计进来）", got)
	}
}
