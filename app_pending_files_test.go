package main

// 拟处理文件清单查询（2026-09-23，设计段 docs/superpowers/specs/2026-09-23-pending-files-query-design.md §5）。
//
// 新绑定在改前树编译不过 ⇒ 按仓内口径（第二批口径 1），这些用例的"修前必红"
// 全部由变异顶（V-1~V-5 见设计段 §5），本文件负责把每格变异各自钉死：
//   - 一致性：清单与预览同源（镜像腿，钉 V-1/V-3 的判据漂移）；
//   - reason 三分：keep/outside/gone 各归各格（钉 V-3）；
//   - 分页钳位与溢出（钉 V-2，M10a 同形）；
//   - 排序稳定与 tie-break（钉 V-4/V-5，M144 同形）；
//   - 与就地清理并发无 race（AS-R3/预览同族）。

import (
	"math"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"filededup/internal/model"
)

// pendingRowsByFlag 把分页结果按 Pending 拆成两个 ID 集，供"与预览互镜像"断言用。
func pendingRowsByFlag(rows []PendingRow) (pend []uint64, excl []uint64) {
	for _, r := range rows {
		if r.Pending {
			pend = append(pend, r.ID)
		} else {
			excl = append(excl, r.ID)
		}
	}
	return pend, excl
}

func sortedUint64(in []uint64) []uint64 {
	out := append([]uint64(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// 一致性腿：同一输入下，清单的「将处理」集必须与 PreviewProcessPolicy 的
// EffectiveIDs **逐 id 相等**（排序腿拆成多条用例会让"集合相等"被序掩盖，
// 这里显式排序后比集合，两腿各说各的话）。
func TestPendingFilesMirrorsPreviewKernel(t *testing.T) {
	a, _, _, insideDir, outsideDir := procFixture(t)
	keepID, redundantID := keepAndStaleIDs(t, a)
	const staleID = uint64(1 << 40)
	inside := idsInDir(t, a, insideDir)

	cases := []struct {
		name string
		dirs []string
		sel  []uint64
	}{
		{"未启用策略", nil, []uint64{keepID, redundantID, staleID}},
		{"启用策略混合勾选", []string{insideDir}, []uint64{keepID, redundantID, staleID, inside.ids[0]}},
		{"仅未命中目录", []string{outsideDir}, []uint64{redundantID, keepID}},
		{"重复勾选", nil, []uint64{redundantID, redundantID, keepID}},
		{"空勾选", nil, nil},
		{"多目录并集", []string{insideDir, outsideDir}, []uint64{keepID, redundantID, staleID}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pv, err := a.PreviewProcessPolicy(tc.dirs, tc.sel)
			if err != nil {
				t.Fatal(err)
			}
			pg, err := a.GetPendingFiles(PendingQuery{Dirs: tc.dirs, SelectedIDs: tc.sel})
			if err != nil {
				t.Fatal(err)
			}
			pend, _ := pendingRowsByFlag(pg.Rows)
			if pg.PendingCount != pv.EffectiveCount || len(pend) != pv.EffectiveCount {
				t.Fatalf("计数失真：清单 PendingCount=%d 当页 %d 行，预览 EffectiveCount=%d",
					pg.PendingCount, len(pend), pv.EffectiveCount)
			}
			if !reflect.DeepEqual(sortedUint64(pend), sortedUint64(pv.EffectiveIDs)) {
				t.Fatalf("两腿判据漂移：清单 %v vs 预览 %v（必须同一内核）",
					sortedUint64(pend), sortedUint64(pv.EffectiveIDs))
			}
		})
	}
}

// reason 三分：dirs 启用时被排除的勾选各归 keep/outside/gone；
// 未启用时目录外冗余项照常是 pending（outside 腿不触发）。
func TestPendingFilesReasonClassification(t *testing.T) {
	a, _, _, insideDir, outsideDir := procFixture(t)
	keepID, _ := keepAndStaleIDs(t, a)
	const staleID = uint64(1 << 40)
	inside := idsInDir(t, a, insideDir)
	outsideID, _ := idInDir(t, a, outsideDir)

	sel := []uint64{keepID, inside.ids[0], outsideID, staleID}

	// 启用策略：inside 命中、outside 落空、keep 硬拒、stale 不在结果集。
	pg, err := a.GetPendingFiles(PendingQuery{Dirs: []string{insideDir}, SelectedIDs: sel})
	if err != nil {
		t.Fatal(err)
	}
	row := map[uint64]PendingRow{}
	for _, r := range pg.Rows {
		row[r.ID] = r
	}
	if pg.PendingCount != 1 || pg.KeepCount != 1 || pg.OutsideCount != 1 || pg.GoneCount != 1 {
		t.Fatalf("四类计数 = %d/%d/%d/%d，应为 1/1/1/1（sel=%v）",
			pg.PendingCount, pg.KeepCount, pg.OutsideCount, pg.GoneCount, sel)
	}
	if pg.Total != 4 {
		t.Fatalf("Total=%d，应为 4（四类各一）", pg.Total)
	}
	if r := row[keepID]; r.Reason != PendingKeep || r.Pending || !strings.HasSuffix(r.Path, "keep.bin") {
		t.Fatalf("保留项归类失真：%+v", r)
	}
	if r := row[outsideID]; r.Reason != PendingOutside || !strings.HasSuffix(r.Path, "c.bin") {
		t.Fatalf("目录外归类失真：%+v", r)
	}
	if r := row[staleID]; r.Reason != PendingGone || r.Path != "" || r.Name != "" {
		// gone 行只有 ID+原因是诚实读数：路径无从得知（byID 已无此项）。
		// 若这里读到非空 path，说明实现"编"了一个来源不明的路径。
		t.Fatalf("gone 行必须只带 ID+原因：%+v", r)
	}
	if r := row[inside.ids[0]]; !r.Pending || r.Reason != "" || r.GroupID == 0 {
		t.Fatalf("命中行失真：%+v", r)
	}

	// 未启用策略：outside 落回 pending，outside 计数归零。
	pg2, err := a.GetPendingFiles(PendingQuery{SelectedIDs: sel})
	if err != nil {
		t.Fatal(err)
	}
	if pg2.PendingCount != 2 || pg2.OutsideCount != 0 || pg2.KeepCount != 1 || pg2.GoneCount != 1 {
		t.Fatalf("未启用时计数 = %d/%d/%d/%d，应为 2/0/1/1",
			pg2.PendingCount, pg2.OutsideCount, pg2.KeepCount, pg2.GoneCount)
	}
	// 前段/后段分界：行序必须 pending 段在前（前端据此插分区标题）。
	if !pg2.Rows[0].Pending || !pg2.Rows[1].Pending || pg2.Rows[2].Pending || pg2.Rows[3].Pending {
		t.Fatalf("行序分界失真：%+v", pg2.Rows)
	}
}

// pendingApp 造多组手工结果集（含 byID 登记），keepIDs 留空 = 全部非保留。
// 发放序与 ID 序刻意错开（M144 手法）：tie-break 被删时落到的是分区序不是 ID 序。
func pendingApp(t *testing.T, mk func(id uint64) *model.DuplicateGroup) *App {
	t.Helper()
	a := newTestApp(t)
	for id := uint64(7); id >= 1; id-- {
		g := mk(id)
		a.groups = append(a.groups, g)
		for _, f := range g.Files {
			a.byID[f.ID] = f
		}
	}
	return a
}

func allFileIDs(a *App) []uint64 {
	var ids []uint64
	for _, g := range a.groups {
		for _, f := range g.Files {
			ids = append(ids, f.ID)
		}
	}
	return ids
}

// group 排序腿：默认序 = 结果集组序（组 1 在前），与勾选传入序无关。
// size 全平手 ⇒ size 腿只能由 GroupID→ID 兜底定序；两次调用逐行相等（稳定性）。
func TestPendingFilesSortLegs(t *testing.T) {
	a := pendingApp(t, func(id uint64) *model.DuplicateGroup {
		return mkGroup(id, 100, "/x/a.bin", "/x/b.bin")
	})
	sel := allFileIDs(a)
	// 勾选序打乱成降序，验证输出与输入序解耦。
	shuffled := append([]uint64(nil), sel...)
	sort.Slice(shuffled, func(i, j int) bool { return shuffled[i] > shuffled[j] })

	// group 腿：组发放序是 7..1，组序键 (grpIdx) 即发放位序 ⇒ 输出组 7,6,...,1，
	// 且组内保持成员序（a.bin 在 b.bin 前）。★ 必须逐行钉完整 ID 序：勾选传入序是
	// ID 降序，GroupID 单调就恰好成立——只判单调的话，"comparator 恒 false、整个
	// 退化成勾选序"这一类变异会漏网（V-5 的正是这一格）。
	gp, err := a.GetPendingFiles(PendingQuery{SelectedIDs: shuffled, Sort: "group", PageSize: 500})
	if err != nil {
		t.Fatal(err)
	}
	if gp.Total != 14 || gp.PendingCount != 14 {
		t.Fatalf("全量计数 total=%d pending=%d，应为 14/14", gp.Total, gp.PendingCount)
	}
	wantGroup := make([]uint64, 0, 14)
	for gid := uint64(7); gid >= 1; gid-- {
		wantGroup = append(wantGroup, gid*100, gid*100+1)
	}
	gotGroup := make([]uint64, 0, len(gp.Rows))
	for _, r := range gp.Rows {
		if !r.Pending {
			t.Fatalf("第 %d 行不该是排除行：%+v", len(gotGroup), r)
		}
		gotGroup = append(gotGroup, r.ID)
	}
	if !reflect.DeepEqual(gotGroup, wantGroup) {
		t.Fatalf("group 腿失真：got=%v want=%v（组序=发放序 7→1、组内保持成员序；退化成勾选序也长这样吗？不——勾选传入是降序）", gotGroup, wantGroup)
	}

	// size 腿全平手 ⇒ GroupID 升序兜底 1..7；组内保持成员序 a.bin→b.bin。
	sp, err := a.GetPendingFiles(PendingQuery{SelectedIDs: shuffled, Sort: "size", PageSize: 500})
	if err != nil {
		t.Fatal(err)
	}
	want := make([]uint64, 0, 14)
	for gid := uint64(1); gid <= 7; gid++ {
		want = append(want, gid*100, gid*100+1)
	}
	got := make([]uint64, 0, len(sp.Rows))
	for _, r := range sp.Rows {
		got = append(got, r.ID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("size 腿平手兜底失真：got=%v want=%v（M144 同格：删 GroupID 兜底必红）", got, want)
	}
	// 第二次调用逐行相等：同输入两页必须一模一样（翻页不跳行）。
	sp2, err := a.GetPendingFiles(PendingQuery{SelectedIDs: shuffled, Sort: "size", PageSize: 500})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sp.Rows, sp2.Rows) {
		t.Fatal("同输入两次读数不等 ⇒ 排序不稳定")
	}

	// path 腿：同路径字符串时按 ID 升序兜底（/x/a.bin 的 7 个组实例按 ID 100..700）。
	pp, err := a.GetPendingFiles(PendingQuery{SelectedIDs: shuffled, Sort: "path", PageSize: 500})
	if err != nil {
		t.Fatal(err)
	}
	var prevPath string
	var prevID uint64
	for _, r := range pp.Rows {
		if r.Path < prevPath || (r.Path == prevPath && r.ID <= prevID) {
			t.Fatalf("path 腿未按 (path, ID) 升序：%+v", r)
		}
		prevPath, prevID = r.Path, r.ID
	}
}

// 分页钳位与溢出（M10a 同形）：PageSize 默认 100 / 钳 500；溢出起点给空页、
// 计数照实；越界页不 panic。
func TestPendingFilesPagingClampAndOverflow(t *testing.T) {
	a := pendingApp(t, func(id uint64) *model.DuplicateGroup {
		return mkGroup(id, 100, "/x/a.bin", "/x/b.bin")
	})
	sel := allFileIDs(a)

	base, err := a.GetPendingFiles(PendingQuery{SelectedIDs: sel, PageSize: 500})
	if err != nil {
		t.Fatal(err)
	}
	if base.Total != 14 {
		t.Fatalf("前提失真 total=%d want 14", base.Total)
	}

	cases := []struct {
		name      string
		page, sz  int
		wantSize  int
		wantPages int
	}{
		{"零值默认 100", 0, 0, 14, 100},
		{"负数默认 100", -3, -1, 14, 100},
		{"超限钳 500", 0, 99999, 14, 500},
	}
	for _, tc := range cases {
		pg, err := a.GetPendingFiles(PendingQuery{SelectedIDs: sel, Page: tc.page, PageSize: tc.sz})
		if err != nil {
			t.Fatal(err)
		}
		if len(pg.Rows) != tc.wantSize || pg.PageSize != tc.wantPages {
			t.Fatalf("%s：行数=%d PageSize=%d，应为 %d/%d", tc.name, len(pg.Rows), pg.PageSize, tc.wantSize, tc.wantPages)
		}
		if pg.Total != 14 || pg.PendingCount != 14 {
			t.Fatalf("%s：计数必须全量口径，实得 total=%d pending=%d", tc.name, pg.Total, pg.PendingCount)
		}
	}

	for _, q := range []PendingQuery{
		{SelectedIDs: sel, Page: 1 << 62, PageSize: 2},
		{SelectedIDs: sel, Page: math.MaxInt, PageSize: 2},
		{SelectedIDs: sel, Page: 9, PageSize: 2}, // 不溢出，但起点越界
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("page=%d pageSize=%d 不该 panic：%v", q.Page, q.PageSize, r)
				}
			}()
			pg, err := a.GetPendingFiles(q)
			if err != nil {
				t.Fatal(err)
			}
			if len(pg.Rows) != 0 || pg.Total != 14 || pg.PendingCount != 14 {
				t.Fatalf("越界/溢出起点应给空页且计数如实：%+v", pg)
			}
			if pg.Rows == nil {
				t.Fatal("空页 Rows 必须是 [] 不是 nil（JSON 直达前端，null 会炸渲染）")
			}
		}()
	}
}

// 与执行器就地清理并发的数据竞争回归（预览同族，AS-R3 锁语义）：
// GetPendingFiles 不受 opsRunning 互斥限制 ⇒ 与清理并发是设计内场景。
func TestGetPendingFilesConcurrentWithCleanupNoRace(t *testing.T) {
	a, _, _, insideDir, _ := procFixture(t)
	ids := func() []uint64 {
		a.mu.Lock()
		defer a.mu.Unlock()
		return allFileIDs(a)
	}()
	if len(ids) < 3 {
		t.Fatalf("夹具至少应有 3 个候选文件，实际 %d", len(ids))
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			a.mu.Lock()
			for _, g := range a.groups {
				if len(g.Files) < 2 {
					continue
				}
				files := g.Files[:0]
				for _, f := range g.Files {
					if !strings.HasSuffix(f.Path, "a.bin") {
						files = append(files, f)
					}
				}
				g.Files = files
			}
			a.mu.Unlock()
		}
	}()

	for i := 0; i < 300; i++ {
		if _, err := a.GetPendingFiles(PendingQuery{Dirs: []string{insideDir}, SelectedIDs: ids}); err != nil {
			t.Fatalf("GetPendingFiles 出错: %v", err)
		}
	}
	close(stop)
	wg.Wait()
}
