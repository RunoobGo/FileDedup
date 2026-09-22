package dedup

// M137（第 3 轮 §24.4 DDP-3）：排序**第二级键**（成员数降序，pipeline.go:809-811）
// 每一对组比较都会被执行到，却从来没有 decisive 过。现有夹具 tiePayloads 是
// A=3×300KiB、B=C=2×600KiB：删掉整级之后 A 仍由第三级键落在原位
// （组内最小路径 m-a1 < y-c1 < z-b1），输出序逐格相同 ⇒ 变异 R3-MU3 全绿
// （§24.2 的真读数），:801 注释承诺的「其次组大小」因此是一格没人钉的假话。
//
// 本用例自带一份「成员数与组内最小路径互相打架」的夹具，把那一格钉死：
//
//	P = 2×300KiB，头 a-p1.bin；Q = 3×150KiB，头 b-q1.bin
//	第一级（可释放量降序）：P (2-1)×300KiB = Q (3-1)×150KiB = 300KiB ⇒ 平手
//	第二级（成员数降序）  ：Q 3 个 > P 2 个 ⇒ 只有这一级能把 Q 提到前面
//	第三级（组内最小路径）：a-p1.bin < b-q1.bin ⇒ 第二级一旦被删，P 就被提前
//
// 期望序 [Q, P] 完全由第二级给出 ⇒ 删 pipeline.go:809-811 必须红在本用例，
// 而 TestGroupOrderAndIDsAreDeterministic 继续绿（那个反差正是 M137 记的形状）。
// ★ 「可释放量相同、成员数不同」这一格由本用例独家钉住，M137 钉的就是这一格；
// 既有那条的 want 一字不改，它继续钉第一、三级。

import (
	"path/filepath"
	"testing"
)

func TestGroupOrderSecondKeyIsDecisive(t *testing.T) {
	root := t.TempDir()
	p := make([]byte, 300<<10)
	q := make([]byte, 150<<10)
	for i := range p {
		p[i] = byte('P' + i%26)
	}
	for i := range q {
		q[i] = byte('Q' + (i+3)%26) // 与 p 内容、长度都不同 ⇒ 两组不会并成一组
	}
	writeSame(t, []string{
		filepath.Join(root, "a-p1.bin"), filepath.Join(root, "a-p2.bin"),
	}, p)
	writeSame(t, []string{
		filepath.Join(root, "b-q1.bin"), filepath.Join(root, "b-q2.bin"), filepath.Join(root, "b-q3.bin"),
	}, q)

	groups := runOnce(t, root)
	if len(groups) != 2 {
		t.Fatalf("组数 = %d, want 2（夹具造了两组）：%+v", len(groups), groups)
	}
	// 夹具前提一：第一级必须平手，否则第二级根本轮不到执行。
	if groups[0].Reclaimable != groups[1].Reclaimable {
		t.Fatalf("夹具前提不成立：两组可释放量不等（%d vs %d），第一级键就把它们分开了，测不到第二级那一格",
			groups[0].Reclaimable, groups[1].Reclaimable)
	}
	if groups[0].Reclaimable != 300<<10 {
		t.Fatalf("夹具前提不成立：可释放量 = %d, want %d（2×300KiB 与 3×150KiB 都该释放 300KiB）",
			groups[0].Reclaimable, int64(300<<10))
	}
	// 夹具前提二：成员数必须不同，否则第二级依旧是死格。
	if len(groups[0].Files) == len(groups[1].Files) {
		t.Fatalf("夹具前提不成立：两组成员数相同（都是 %d），测不到「其次组大小」那一格", len(groups[0].Files))
	}

	type cell struct {
		n    int
		head string
		recl uint64
	}
	// 期望的精确顺序：成员数多的 Q 在前，尽管它的最小路径更大。
	want := []cell{
		{n: 3, head: "b-q1.bin", recl: 300 << 10},
		{n: 2, head: "a-p1.bin", recl: 300 << 10},
	}
	for k, w := range want {
		g := groups[k]
		if len(g.Files) == 0 {
			t.Fatalf("空组进了结果集：第 %d 位 GroupID=%d", k+1, g.GroupID)
		}
		if g.GroupID != uint64(k+1) {
			t.Fatalf("第 %d 位 GroupID = %d，want %d（组号须在排序后按序号发放）", k+1, g.GroupID, k+1)
		}
		if g.Reclaimable != w.recl {
			t.Fatalf("第 %d 位可释放量 = %d, want %d", k+1, g.Reclaimable, w.recl)
		}
		if len(g.Files) != w.n || filepath.Base(g.Files[0].Path) != w.head {
			t.Fatalf("第 %d 位 = 成员数 %d / 头 %s, want 成员数 %d / 头 %s："+
				"两组的可释放量相同、而最小路径序与成员数序相反，"+
				"这个先后只能由第二级键（成员数降序）给出 ⇒ M137 钉的就是这一格",
				k+1, len(g.Files), filepath.Base(g.Files[0].Path), w.n, w.head)
		}
	}
}
