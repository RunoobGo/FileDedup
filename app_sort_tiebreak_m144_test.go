package main

// M144（04 §6.11 TST-6，第 3 轮 §24.4）：buildSortedGroupsLocked 的三个分支在"主键平手"
// 时都以 `GroupID <` 兜底（app.go:899 / :906 / :913，注释 :892 明写"稳定：组 ID 兜底"）。
// 改前没有一条用例把"平手"造出来 ⇒ 三个兜底都是删得掉而门禁不响的死代码：
// 把它们各自改成 `return false`，整个包照绿。本条按分支逐格钉住。
//
// 夹具的发放顺序**故意与 ID 序相反**：`sort.Slice` 不是稳定排序，兜底一旦被删，
// 平手元素之间不再有任何比较依据，落到的序取决于发放序与分区过程而不是 ID。
// 断言取"完整 ID 序列逐一相等"而非只看首尾，免得偶然的局部序蒙混过关。

import (
	"testing"

	"filededup/internal/model"
)

// descendingApp 造 7 组，按 ID 降序发放进 a.groups（7,6,5,...,1）。
// build 只需给出"该分支主键彼此平手"的组，其余字段可以随便错开。
func descendingApp(t *testing.T, build func(id uint64) *model.DuplicateGroup) *App {
	t.Helper()
	a := newTestApp(t)
	for id := uint64(7); id >= 1; id-- {
		a.groups = append(a.groups, build(id))
	}
	// 夹具前提自检（不经判据本体）：发放序必须是 ID 降序，
	// 否则"删兜底即红"的构造根本不成立。
	for i, g := range a.groups {
		if want := uint64(7 - i); g.GroupID != want {
			t.Fatalf("夹具前提不成立：发放序第 %d 位是 %d，应为 %d", i, g.GroupID, want)
		}
	}
	return a
}

func expectAscendingIDs(t *testing.T, label string, r PagedResult) {
	t.Helper()
	if len(r.Groups) != 7 {
		t.Fatalf("%s：组数 = %d，应为 7 ⇒ 夹具没进排序（本条没取到读数）", label, len(r.Groups))
	}
	got := make([]uint64, 0, 7)
	for _, g := range r.Groups {
		got = append(got, g.GroupID)
	}
	for i, id := range got {
		if id != uint64(i+1) {
			t.Fatalf("%s：平手时未按 GroupID 兜底 ⇒ 得到 %v，期望 [1 2 3 4 5 6 7]（M144）。"+
				"若兜底被删，这里落到的是发放序/pdqsort 分区序，注释承诺的「稳定」即为假", label, got)
		}
	}
}

// size 分支：主键 Files[0].Size 全部相等 ⇒ 只能靠 GroupID 兜底定序。
func TestSortTiebreakByIDSizeBranch(t *testing.T) {
	a := descendingApp(t, func(id uint64) *model.DuplicateGroup {
		return mkGroup(id, 100, "a.bin", "b.bin")
	})
	r, err := a.GetResultGroups(ResultQuery{Sort: "size"})
	if err != nil {
		t.Fatal(err)
	}
	expectAscendingIDs(t, "size", r)
}

// count 分支：成员数全等，但体积/可释放量彼此错开 ⇒ 结果序不可能由 size 解释，
// 只能由兜底键给出。
func TestSortTiebreakByIDCountBranch(t *testing.T) {
	a := descendingApp(t, func(id uint64) *model.DuplicateGroup {
		return mkGroup(id, 100*id, "a.bin", "b.bin", "c.bin")
	})
	r, err := a.GetResultGroups(ResultQuery{Sort: "count"})
	if err != nil {
		t.Fatal(err)
	}
	expectAscendingIDs(t, "count", r)
}

// reclaimable 分支（default）：Reclaimable 一律凑成 200（偶数 ID 用 2 个 200B 文件、
// 奇数 ID 用 3 个 100B 文件），体积与成员数则在两组之间来回错开。
func TestSortTiebreakByIDReclaimableBranch(t *testing.T) {
	a := descendingApp(t, func(id uint64) *model.DuplicateGroup {
		if id%2 == 0 {
			return mkGroup(id, 200, "a.bin", "b.bin")
		}
		return mkGroup(id, 100, "a.bin", "b.bin", "c.bin")
	})
	r, err := a.GetResultGroups(ResultQuery{})
	if err != nil {
		t.Fatal(err)
	}
	expectAscendingIDs(t, "reclaimable", r)
	for i, g := range r.Groups {
		if g.Reclaimable != 200 {
			t.Fatalf("夹具前提不成立：第 %d 组 Reclaimable = %d，主键并未平手", i, g.Reclaimable)
		}
	}
}
