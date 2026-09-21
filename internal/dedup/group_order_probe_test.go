package dedup

// M94（2026-09-22 三轮审查第 1 轮 / 设计稿 §22.3）：组序与组号在同输入下必须确定。
//
// 判据格只有一处：**两组 Reclaimable 恰好相等**的那一格。不等时降序排序本来就
// 确定，测不出任何东西；相等时排序退到次级键，而改前的次级键是 Run 里由 map 随机
// 遍历序发放的 GroupID 本身（pipeline.go:765 range + :791 id++），所以组序与组号
// 两跑之间都会漂。夹具因此做成「可释放量相同、成员数不同」：
// 3×300KiB 与 2×600KiB 都可释放 600KiB，但组大小不同——这也是 :800 注释承诺
// 「其次组大小」却从未参与的那一格的实物见证。
//
// 连跑 10 次而不是 2 次：map 遍历序每次迭代都重新随机化，两组只剩先后两态，
// 两跑一致的期望概率是 1/2，只比两跑有约一半机会漏红。

import (
	"path/filepath"
	"testing"

	"filededup/internal/model"
)

// tiePayloads 造两组同可释放量、不同成员数的重复文件。
func tiePayloads(t *testing.T, root string) {
	t.Helper()
	a := make([]byte, 300<<10)
	b := make([]byte, 600<<10)
	for i := range a {
		a[i] = byte('A' + i%26)
	}
	for i := range b {
		b[i] = byte('B' + i%26)
	}
	// 名字刻意交叉，避免「组内最小路径的字典序恰好等于哈希桶序」这种侥幸把漂电压低。
	writeSame(t, []string{
		filepath.Join(root, "m-a1.bin"), filepath.Join(root, "m-a2.bin"), filepath.Join(root, "m-a3.bin"),
	}, a)
	writeSame(t, []string{
		filepath.Join(root, "z-b1.bin"), filepath.Join(root, "z-b2.bin"),
	}, b)
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
	if len(first) != 2 {
		t.Fatalf("组数 = %d, want 2（夹具造了两组）：%+v", len(first), first)
	}
	if first[0].recl != first[1].recl {
		t.Fatalf("夹具前提不成立：两组可释放量不相等（%d vs %d），判据落不到次级键那一格",
			first[0].recl, first[1].recl)
	}
	// 前提自检只看夹具自己的形状，不经被测的排序判据（I5）。
	if first[0].n == first[1].n {
		t.Fatalf("夹具前提不成立：两组成员数相同（都是 %d），测不到「其次组大小」那一格", first[0].n)
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
	if len(groups) != 2 {
		t.Fatalf("组数 = %d, want 2", len(groups))
	}
	for i, g := range groups {
		if g.GroupID != uint64(i+1) {
			t.Fatalf("第 %d 位的 GroupID = %d，want %d：组号必须是排序之后按序号发放（1..N），"+
				"否则组号把 map 的随机遍历序漏进了输出", i+1, g.GroupID, i+1)
		}
	}
}
