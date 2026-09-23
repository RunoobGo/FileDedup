package main

// 功能 3（2026-09-23）：「隐藏非拟处理项」的显示投影——GetResultGroups 给
// FileView.IsPending 填真值。
//
// 核心纪律与 AS-H6/I5 同族：**前端不重算判据**。"这一行会不会被动"只由
// pendingIDsLocked 决定；结果页只是它的一个投影（不带勾选上下文的
// "策略可处理"口径：非保留 ∩ 白名单 − 黑名单）。若前端自己拿 isKeep/路径
// 再拼一份，界面藏起来的和引擎留下的就是两套语义——正是那两类事故的形状。
//
// ★ 为什么必须镜像核对 PreviewProcessPolicy（H1/H6）：IsPending 走的是
// "全量候选集"的 pendingIDsLocked，执行走的是"勾选候选集"的那一次。两条路
// 都声称出自同一内核，只有把同一批 ID 分别喂给两边再对表，才证得出没漂移。
//
// ★ 为什么钉"策略不改变分组与聚合"（H5）：本功能只许藏行，不许改账——
// Total/Reclaimable 若随投影口径浮动，统计条就成了"部分当全部"（M4 教训）。

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// eqIDs 比较两组 ID（都先升序归一），DeepEqual 对 nil vs 空切片也判不等，
// 所以两侧都过 sortedUint64。
func eqIDs(a, b []uint64) bool {
	return reflect.DeepEqual(sortedUint64(a), sortedUint64(b))
}

// idsExcept 返回 a 中不在 b 里的元素（保持原顺序）。
func idsExcept(a, b []uint64) []uint64 {
	in := map[uint64]bool{}
	for _, id := range b {
		in[id] = true
	}
	var out []uint64
	for _, id := range a {
		if !in[id] {
			out = append(out, id)
		}
	}
	return out
}

// isPendingRows 取全量结果页并按 ID 索引（ID 重复直接判失败）。
func isPendingRows(t *testing.T, a *App, q ResultQuery) map[uint64]FileView {
	t.Helper()
	q.PageSize = 500
	r, err := a.GetResultGroups(q)
	if err != nil {
		t.Fatal(err)
	}
	rows := map[uint64]FileView{}
	for _, g := range r.Groups {
		for _, f := range g.Files {
			if _, dup := rows[f.ID]; dup {
				t.Fatalf("结果页出现重复 ID %d", f.ID)
			}
			rows[f.ID] = f
		}
	}
	return rows
}

// pendingTrueIDs 汇出 IsPending=true 的 ID（升序）。
func pendingTrueIDs(rows map[uint64]FileView) []uint64 {
	var out []uint64
	for id, f := range rows {
		if f.IsPending {
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// keepFalseNonKeep 把结果拆成 保留项 ID 与 非保留项 ID（升序）。
func keepSplit(rows map[uint64]FileView) (keep, nonKeep []uint64) {
	for id, f := range rows {
		if f.IsKeep {
			keep = append(keep, id)
		} else {
			nonKeep = append(nonKeep, id)
		}
	}
	sort.Slice(keep, func(i, j int) bool { return keep[i] < keep[j] })
	sort.Slice(nonKeep, func(i, j int) bool { return nonKeep[i] < nonKeep[j] })
	return
}

func TestIsPendingNoPolicyHidesOnlyKeep(t *testing.T) {
	a, _, _, _, _ := procFixture(t)
	rows := isPendingRows(t, a, ResultQuery{})
	keep, nonKeep := keepSplit(rows)
	if len(keep) == 0 {
		t.Fatal("夹具没有保留项，H1 空转")
	}
	for _, id := range keep {
		if rows[id].IsPending {
			t.Errorf("保留项 %d IsPending=true——执行器对保留项硬拒绝，它永远不会被处理", id)
		}
	}
	got := pendingTrueIDs(rows)
	if !eqIDs(got, nonKeep) {
		t.Errorf("无策略时拟处理真值应=全部非保留项\ngot  %v\nwant %v", got, nonKeep)
	}
	// 镜像：同一批 ID 喂给预览内核，两边必须逐位一致
	pv, err := a.PreviewProcessPolicy(nil, nil, nonKeep)
	if err != nil {
		t.Fatal(err)
	}
	if !eqIDs(got, pv.EffectiveIDs) {
		t.Errorf("显示投影与预览内核漂移：投影 %v vs 内核 %v", got, pv.EffectiveIDs)
	}
}

func TestIsPendingWithDirsAndExcludeDirs(t *testing.T) {
	a, _, _, insideDir, outsideDir := procFixture(t)
	rows := isPendingRows(t, a, ResultQuery{})
	_, nonKeep := keepSplit(rows)
	inAll := idsInDir(t, a, insideDir)   // a.bin + sub/b.bin
	outAll := idsInDir(t, a, outsideDir) // c.bin
	subDir := filepath.Join(insideDir, "sub")
	subIDs := idsInDir(t, a, subDir).ids // 只有 b.bin
	if len(subIDs) != 1 || len(outAll.ids) != 1 {
		t.Fatalf("夹具前提漂移：sub=%v outside=%v", subIDs, outAll.ids)
	}

	cases := []struct {
		name        string
		dirs, exDir []string
		wantPending []uint64
	}{
		{"仅白名单：目录外全灭", []string{insideDir}, nil, inAll.ids},
		{"仅黑名单：命中的灭（目录外不受牵连）", nil, []string{subDir}, idsExcept(nonKeep, subIDs)},
		{"黑白串联：先白后黑", []string{insideDir}, []string{subDir}, idsExcept(inAll.ids, subIDs)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pendingTrueIDs(isPendingRows(t, a, ResultQuery{
				Dirs: tc.dirs, ExcludeDirs: tc.exDir,
			}))
			if !eqIDs(got, tc.wantPending) {
				t.Errorf("IsPending 集合不符\ngot  %v\nwant %v", got, sortedUint64(tc.wantPending))
			}
			// 每种组合都过一遍镜像对表（投影与执行判据不许有第二份实现）
			pv, err := a.PreviewProcessPolicy(tc.dirs, tc.exDir, nonKeep)
			if err != nil {
				t.Fatal(err)
			}
			if !eqIDs(got, pv.EffectiveIDs) {
				t.Errorf("投影与预览内核漂移：投影 %v vs 内核 %v", got, sortedUint64(pv.EffectiveIDs))
			}
		})
	}
}

func TestIsPendingKeepsKeepAnchorsVisibleInPolicy(t *testing.T) {
	// 保留项即使在白名单内也 IsPending=false（H1 已证），但它**不该被投影改标记**：
	// IsKeep 必须原样，否则"藏非拟处理项"会把保留锚也一并藏进错误的理由里。
	a, _, _, insideDir, _ := procFixture(t)
	rows := isPendingRows(t, a, ResultQuery{Dirs: []string{insideDir}})
	keep, _ := keepSplit(rows)
	for _, id := range keep {
		if !rows[id].IsKeep {
			t.Errorf("保留项 %d 的 IsKeep 被投影改写了", id)
		}
	}
}

func TestIsPendingDoesNotChangeAggregates(t *testing.T) {
	a, _, _, insideDir, _ := procFixture(t)
	base, err := a.GetResultGroups(ResultQuery{PageSize: 500})
	if err != nil {
		t.Fatal(err)
	}
	withPolicy, err := a.GetResultGroups(ResultQuery{PageSize: 500, Dirs: []string{insideDir}})
	if err != nil {
		t.Fatal(err)
	}
	if withPolicy.Total != base.Total ||
		withPolicy.TotalReclaimable != base.TotalReclaimable ||
		withPolicy.TotalReclaimableActual != base.TotalReclaimableActual {
		t.Errorf("投影改变了聚合口径：Total %d→%d, Reclaimable %d→%d",
			base.Total, withPolicy.Total, base.TotalReclaimable, withPolicy.TotalReclaimable)
	}
	if len(withPolicy.Groups) != len(base.Groups) {
		t.Errorf("分组数不该随策略变化：%d→%d（藏行是显示层的事，不是查询层）",
			len(base.Groups), len(withPolicy.Groups))
	}
}

func TestIsPendingAcrossPages(t *testing.T) {
	// 分页只切组不切判据：同一 ID 在第 0 页和第 1 页（若出现）IsPending 必须同值，
	// 且各页合起来与单页全量口径一致。
	a, _, _, insideDir, _ := procFixture(t)
	full := isPendingRows(t, a, ResultQuery{Dirs: []string{insideDir}})
	var paged map[uint64]FileView = map[uint64]FileView{}
	for page := 0; ; page++ {
		r, err := a.GetResultGroups(ResultQuery{Page: page, PageSize: 1, Dirs: []string{insideDir}})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Groups) == 0 {
			break
		}
		for _, g := range r.Groups {
			for _, f := range g.Files {
				paged[f.ID] = f
			}
		}
		if page > 10 {
			t.Fatal("分页没有尽头？")
		}
	}
	if len(paged) != len(full) {
		t.Fatalf("分页汇总 %d 行 ≠ 全量 %d 行", len(paged), len(full))
	}
	for id, f := range paged {
		if f.IsPending != full[id].IsPending {
			t.Errorf("ID %d 在第 0 页 IsPending=%v、分页读=%v（同屏两套真值）",
				id, full[id].IsPending, f.IsPending)
		}
	}
}
