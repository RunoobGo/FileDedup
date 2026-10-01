package model

// 第九轮修订批 B1（拟 M382）：实占可释放量必须按**默认保留项**扣，而不是按下标 0 扣。
//
// 报告 P1-3 的现读事实：ReclaimActual / actualKnownIn 都写作 `files[1:]`，注释里那句
// "约定 files[0] 为保留项"从来没有被生产者保证过 —— 扫描流水线存组前按**路径字典序**
// 排成员（pipeline_stages.go 的 sortEntries），而默认保留策略选的是"先非隐藏、再路径最短"
// （ops.pickShortest）。两条规则通常选中的不是同一条：于是报出来的可释放实占
// **虚高一份**（把真正留下的那条算进去了）**又少算一份**（把实际会被清掉的那条当保留项）。
//
// ★ 判据形状取自生产路径：隐藏目录 + 字典序最小者不是最短者，正是 sortEntries 之后
//   最常见的那种组。

import "testing"

func m382Entry(id uint64, path string, size, actual uint64, known bool) *FileEntry {
	return &FileEntry{ID: id, Path: path, Size: size, Actual: actual, ActualKnown: known}
}

// P-48：默认保留项不在 index 0 时，扣掉的必须是默认保留项那一条。
func TestReclaimActualSubtractsDefaultKeeperNotIndexZero(t *testing.T) {
	files := []*FileEntry{
		m382Entry(1, "/root/.hidden/a.txt", 1000, 4096, true), // 字典序最小，但隐藏 ⇒ 不是保留项
		m382Entry(2, "/root/keep.txt", 1000, 8192, true),      // 非隐藏且最短 ⇒ 默认保留项
		m382Entry(3, "/root/another.txt", 1000, 2048, true),   // 冗余项
	}
	if keep := PickDefaultKeepIndex(files); keep != 1 {
		t.Fatalf("夹具没铺成预期形状：默认保留项下标 = %d，期望 1（不是 index 0）", keep)
	}
	const want = uint64(4096 + 2048)
	if got := ReclaimActual(files); got != want {
		t.Fatalf("ReclaimActual=%d，want %d —— 按 files[1:] 扣会把默认保留项的 8192 算进来、"+
			"又把实际会被清掉的 4096 当保留项（虚高一份又少算一份）", got, want)
	}
}

// P-49（负控制）：默认保留项恰在 index 0 时，改后与改前逐字节同值。
// 这一格防的是"修一个口径顺手改坏另一个"——改前它就该绿，改后必须还是绿。
func TestReclaimActualUnchangedWhenKeeperIsIndexZero(t *testing.T) {
	files := []*FileEntry{
		m382Entry(1, "/a", 1000, 4096, true), // 最短且在 index 0 ⇒ 新旧约定选中同一条
		m382Entry(2, "/abc", 1000, 8192, true),
		m382Entry(3, "/abcdef", 1000, 2048, true),
	}
	if keep := PickDefaultKeepIndex(files); keep != 0 {
		t.Fatalf("夹具没铺成负控制形状：默认保留项下标 = %d，期望 0", keep)
	}
	const want = uint64(8192 + 2048) // 旧写法 Σ files[1:] 的同值
	if got := ReclaimActual(files); got != want {
		t.Fatalf("ReclaimActual=%d，want %d —— 保留项在 index 0 时新实现必须与旧约定逐字节同值", got, want)
	}
	if g := (&DuplicateGroup{Files: files}); !g.AnyActualKnown() {
		t.Fatal("AnyActualKnown=false，而数字里含两条真读数")
	}
}

// P-50：只有默认保留项读到过实占、冗余项全 unknown ⇒ 标志必须为 false（MODEL-1 的原意）。
// 改前的形状是"标志为真、数字纯是逻辑口径回退值"⇒ 界面把回退值显示成已统计。
func TestAnyActualKnownFalseWhenOnlyDefaultKeeperIsKnown(t *testing.T) {
	files := []*FileEntry{
		m382Entry(1, "/root/.hidden/a.txt", 1000, 0, false),
		m382Entry(2, "/root/keep.txt", 1000, 8192, true), // 默认保留项：有读数，但不参与可释放量
		m382Entry(3, "/root/another.txt", 1000, 0, false),
	}
	g := &DuplicateGroup{Files: files}
	if g.AnyActualKnown() {
		t.Fatalf("AnyActualKnown=true 而 ReclaimActual=%d 纯是逻辑口径回退值 —— 标志与数字必须同一总体（MODEL-1）",
			ReclaimActual(files))
	}
	if want := uint64(1000 + 1000); ReclaimActual(files) != want {
		t.Fatalf("ReclaimActual=%d，want %d（未知按逻辑大小保守计入）", ReclaimActual(files), want)
	}
}

// PickDefaultKeepIndex 本体：与 ops.pickShortest 同尺子的三格边界。
func TestPickDefaultKeepIndexEdges(t *testing.T) {
	cases := []struct {
		name  string
		paths []string
		want  int
	}{
		{"空切片", nil, -1},
		{"单成员", []string{"/x/y"}, 0},
		{"同长度并列取下标小的那条", []string{"/aa", "/bb"}, 0},
		{"隐藏项让位给非隐藏的最长者", []string{"/.h/a", "/very/long/path"}, 1},
		{"全隐藏时仍按最短", []string{"/.a/long", "/.b/x"}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var files []*FileEntry
			for i, p := range c.paths {
				files = append(files, m382Entry(uint64(i+1), p, 10, 10, true))
			}
			if got := PickDefaultKeepIndex(files); got != c.want {
				t.Errorf("PickDefaultKeepIndex=%d，want %d", got, c.want)
			}
		})
	}
}
