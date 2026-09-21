package dedup

// M94（2026-09-22 三轮审查第 1 轮 / 设计稿 §22.3）：组序与组号在同输入下必须确定。
//
// 判据格原本只有一处：**两组 Reclaimable 恰好相等**的那一格。不等时降序排序本来就
// 确定，测不出任何东西；相等时排序退到次级键，而改前的次级键是 Run 里由 map 随机
// 遍历序发放的 GroupID 本身（pipeline.go:765 range + :791 id++），所以组序与组号
// 两跑之间都会漂。夹具因此做成「可释放量相同、成员数不同」：
// 3×300KiB 与 2×600KiB 都可释放 600KiB，但组大小不同——这也是 :800 注释承诺
// 「其次组大小」却从未参与的那一格的实物见证。
//
// ★ M122（第 2 轮 §23.9）：上面那份夹具在**第二级键**就分出胜负，第三级键
// （组内最小路径）整条没有判据覆盖——把 pipeline.go:812 那行删掉本探针照绿，
// 而 :804 "三级用尽后不再有平手"那句承诺随之变成假话。
// 现在加第三组：与第二组**同可释放量、同成员数**（都是 2×600KiB，内容不同所以不成一组），
// 于是这一对的先后只能由第三级决定。判据 = 变异：删第三级键必须红（见 04 §6.19）。
//
// 连跑 10 次而不是 2 次：map 遍历序每次迭代都重新随机化，两组只剩先后两态，
// 两跑一致的期望概率是 1/2，只比两跑有约一半机会漏红。

import (
	"path/filepath"
	"testing"

	"filededup/internal/model"
)

// tiePayloads 造三组重复文件，把排序的三级键各钉在一格上：
//
//	A = 3×300KiB、B = 2×600KiB、C = 2×600KiB（与 B 内容不同）
//	第一级（可释放量降序）：三组都是 600KiB ⇒ 全平手
//	第二级（成员数降序）  ：A 3 个、B/C 各 2 个 ⇒ 只有 A 分出去
//	第三级（组内最小路径）：B 与 C 只剩这一级可分 ⇒ head 小的在前（C 的 y-c1 < B 的 z-b1）
//
// 名字刻意交叉，避免「组内最小路径的字典序恰好等于哈希桶序」这种侥幸把漂电压低。
func tiePayloads(t *testing.T, root string) {
	t.Helper()
	a := make([]byte, 300<<10)
	b := make([]byte, 600<<10)
	c := make([]byte, 600<<10)
	for i := range a {
		a[i] = byte('A' + i%26)
	}
	for i := range b {
		b[i] = byte('B' + i%26)
	}
	for i := range c {
		c[i] = byte('C' + (i+7)%26) // 与 b 同长不同内容：不成同一组，但可释放量相同
	}
	writeSame(t, []string{
		filepath.Join(root, "m-a1.bin"), filepath.Join(root, "m-a2.bin"), filepath.Join(root, "m-a3.bin"),
	}, a)
	writeSame(t, []string{
		filepath.Join(root, "z-b1.bin"), filepath.Join(root, "z-b2.bin"),
	}, b)
	writeSame(t, []string{
		filepath.Join(root, "y-c1.bin"), filepath.Join(root, "y-c2.bin"),
	}, c)
}

func TestGroupOrderAndIDsAreDeterministic(t *testing.T) {
	root := t.TempDir()
	tiePayloads(t, root)

	type cell struct {
		id   uint64
		n    int
		head string
		recl uint64
	}
	seq := func(groups []*model.DuplicateGroup) []cell {
		out := make([]cell, 0, len(groups))
		for _, g := range groups {
			if len(g.Files) == 0 {
				t.Fatalf("空组进了结果集：GroupID=%d", g.GroupID)
			}
			out = append(out, cell{g.GroupID, len(g.Files), g.Files[0].Path, g.Reclaimable})
		}
		return out
	}

	first := seq(runOnce(t, root))
	if len(first) != 3 {
		t.Fatalf("组数 = %d, want 3（夹具造了三组）：%+v", len(first), first)
	}
	for _, g := range first {
		if g.recl != 600<<10 {
			t.Fatalf("夹具前提不成立：某组可释放量 = %d, want %d ⇒ 第一级键就把三组分开了，测不到平手格",
				g.recl, int64(600<<10))
		}
	}
	// 第二级那一格：A 与 B/C 成员数不同。
	if first[0].n == first[1].n {
		t.Fatalf("夹具前提不成立：前两组成员数相同（都是 %d），测不到「其次组大小」那一格", first[0].n)
	}
	// ★ 第三级那一格（M122 的本体）：后两组成员数必须相同，否则第三级键又成了死格。
	if first[1].n != first[2].n {
		t.Fatalf("夹具前提不成立：第 2、3 组成员数不同（%d vs %d），测不到「组内最小路径」那一格",
			first[1].n, first[2].n)
	}
	// 期望顺序本身（不只"两跑一致"）：删掉第三级键后这一对由 map 随机序决定，
	// 变异必须红在这里或下面的连跑比对（两者取其一即算判据活着）。
	want := []cell{
		{id: 1, n: 3, head: "m-a1.bin", recl: 600 << 10},
		{id: 2, n: 2, head: "y-c1.bin", recl: 600 << 10},
		{id: 3, n: 2, head: "z-b1.bin", recl: 600 << 10},
	}
	for k := range want {
		got := first[k]
		if got.id != want[k].id || got.n != want[k].n || got.recl != want[k].recl ||
			filepath.Base(got.head) != want[k].head {
			t.Fatalf("第 %d 位 = %+v, want %+v（三级键的期望顺序：可释放量平手 → 成员数分出 A → 最小路径分出 C 先于 B）",
				k+1, got, want[k])
		}
	}

	for i := 1; i < 10; i++ {
		got := seq(runOnce(t, root))
		if len(got) != len(first) {
			t.Fatalf("第 %d 跑组数 = %d, want %d", i+1, len(got), len(first))
		}
		for k := range first {
			if first[k] != got[k] {
				t.Fatalf("第 %d 跑与第 1 跑在第 %d 位不一致（改前判据：GroupID 由 map 随机遍历序发放）\n"+
					"  第 1 跑：%+v\n  第 %d 跑：%+v", i+1, k, first, i+1, got)
			}
		}
	}
}

func TestGroupIDsAreContiguousFromOne(t *testing.T) {
	root := t.TempDir()
	tiePayloads(t, root)

	groups := runOnce(t, root)
	if len(groups) != 3 {
		t.Fatalf("组数 = %d, want 3（夹具造了三组）", len(groups))
	}
	for i, g := range groups {
		if g.GroupID != uint64(i+1) {
			t.Fatalf("第 %d 位的 GroupID = %d，want %d：组号必须是排序之后按序号发放（1..N），"+
				"否则组号把 map 的随机遍历序漏进了输出", i+1, g.GroupID, i+1)
		}
	}
}
