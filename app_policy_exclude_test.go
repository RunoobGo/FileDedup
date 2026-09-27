package main

// 功能 2（2026-09-23）：「不处理指定目录文件」黑名单探针。
//
// 语义（用户已拍板）：黑名单内文件**照常参与保留判定、可被选为保留锚点、
// 照常出现在结果集**——黑名单的唯一效果是永不进入拟处理集。判据收进
// pendingIDsLocked 一处，预览/清单/执行三条腿同源（AS-H6/I5 教训：
// 判据一旦有两份，界面对"不会动的文件"就会说假话）。
//
// 新参数在改前树编译不过 ⇒ "修前必红"按仓内口径由变异钉顶（同清单查询
// 第二批口径）；本文件负责把每格语义各自钉死：
//   - 预览：黑名单只减不增，空白项 = 未启用；
//   - 清单：reason "excluded" 独立计数，短路序 gone→keep→outside→excluded；
//   - 执行：范围收窄、账本同范围、全落空时明确拒绝且复位 opsRunning；
//   - 结构性边界：黑名单绝不渗进保留引擎/结果集（TestExcludeDirsNever...）。

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"filededup/internal/model"
)

// 预览腿：黑名单把命中项从生效集中扣除，且不牵连其他文件。
func TestPreviewExcludeDirsDropsBlacklisted(t *testing.T) {
	a, _, root, insideDir, outsideDir := procFixture(t)
	insideSub := filepath.Join(insideDir, "sub")
	keepDir := filepath.Join(root, "keepme")

	_, keepID := keepAndRedundant(t, a)
	insideAll := idsInDir(t, a, insideDir)
	subIDs := idsInDir(t, a, insideSub)
	outID, _ := idInDir(t, a, outsideDir)
	if len(insideAll.ids) != 2 || len(subIDs.ids) != 1 {
		t.Fatalf("夹具失真 inside=%v sub=%v", insideAll.paths, subIDs.paths)
	}
	subID := subIDs.ids[0]

	// 未启用白名单：b（黑名单子树内）落空，c 照常生效——黑名单只减不增。
	pv, err := a.PreviewProcessPolicy(nil, []string{insideSub}, []uint64{keepID, subID, outID})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(pv.EffectiveIDs, []uint64{outID}) {
		t.Fatalf("黑名单生效集失真：%v，应为 [%d]（保留项另有判定，b 在黑名单内）", pv.EffectiveIDs, outID)
	}
	if pv.EffectiveCount != 1 {
		t.Fatalf("计数 = %d, want 1", pv.EffectiveCount)
	}

	// 白名单 ∩ 黑名单同目录：交集必然为空。预览只描述结果、不报错
	// （是否拒绝由 ExecuteOperation 决定，与 pv4 同形）。
	pv2, err := a.PreviewProcessPolicy([]string{insideDir}, []string{insideDir}, insideAll.ids)
	if err != nil {
		t.Fatalf("交集为空预览不该报错：%v", err)
	}
	if pv2.EffectiveCount != 0 {
		t.Fatalf("白名单被黑名单整体覆盖时应生效 0，实得 %d/%v", pv2.EffectiveCount, pv2.EffectiveIDs)
	}

	// 黑名单盖住保留者目录：保留项本来就不在生效集，判定不变；
	// 同批勾选的冗余项必须照常生效——黑名单不许"顺带"多吞。
	pv3, err := a.PreviewProcessPolicy(nil, []string{keepDir}, []uint64{keepID, subID})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(pv3.EffectiveIDs, []uint64{subID}) {
		t.Fatalf("黑名单不该牵连非命中项：%v want [%d]", pv3.EffectiveIDs, subID)
	}

	// 全空白目录 = 未启用（与 normalizeDirs 丢弃空白同口径）：勾选原样生效。
	pv4, err := a.PreviewProcessPolicy(nil, []string{"  ", ""}, []uint64{subID, outID})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(pv4.EffectiveIDs, []uint64{subID, outID}) {
		t.Fatalf("空白黑名单应视同未启用：%v want [%d %d]", pv4.EffectiveIDs, subID, outID)
	}
}

// 清单腿：reason "excluded" 各归各格 + 短路序 + 与预览互镜像。
func TestPendingFilesExcludedReason(t *testing.T) {
	a, _, root, insideDir, outsideDir := procFixture(t)
	_, keepID := keepAndRedundant(t, a)
	const staleID = uint64(1 << 40)
	inside := idsInDir(t, a, insideDir)
	outID, _ := idInDir(t, a, outsideDir)
	sel := []uint64{keepID, inside.ids[0], inside.ids[1], outID, staleID}

	// 未启用白名单、黑名单=insideDir（递归含子目录）：a、b 落 excluded，
	// keep/gone 各归各格，c 是唯一 pending。
	pg, err := a.GetPendingFiles(PendingQuery{SelectedIDs: sel, ExcludeDirs: []string{insideDir}})
	if err != nil {
		t.Fatal(err)
	}
	if pg.PendingCount != 1 || pg.KeepCount != 1 || pg.ExcludedCount != 2 ||
		pg.OutsideCount != 0 || pg.GoneCount != 1 {
		t.Fatalf("五类计数 = pending%d/keep%d/excluded%d/outside%d/gone%d，应为 1/1/2/0/1",
			pg.PendingCount, pg.KeepCount, pg.ExcludedCount, pg.OutsideCount, pg.GoneCount)
	}
	if pg.Total != 5 {
		t.Fatalf("Total=%d，应为 5", pg.Total)
	}
	row := map[uint64]PendingRow{}
	for _, r := range pg.Rows {
		row[r.ID] = r
	}
	if r := row[inside.ids[0]]; r.Pending || r.Reason != PendingExcluded || r.Path == "" {
		t.Fatalf("黑名单行失真：%+v（excluded 行必须带路径供人核对）", r)
	}
	if r := row[outID]; !r.Pending || r.Reason != "" {
		t.Fatalf("白名单未启用时目录外冗余项必须照常 pending：%+v", r)
	}
	if !pg.Rows[0].Pending || pg.Rows[1].Pending {
		t.Fatalf("行序分界失真（前 %d 行应为 pending 段）：%+v", pg.PendingCount, pg.Rows)
	}

	// 短路序 outside 先于 excluded：a 既不在白名单也在黑名单 → 记 outside。
	// （白名单没放行，黑名单根本没轮到它——归类要说"最先拦住它的是谁"。）
	pg2, err := a.GetPendingFiles(PendingQuery{
		Dirs: []string{outsideDir}, ExcludeDirs: []string{insideDir},
		SelectedIDs: []uint64{inside.ids[0], outID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if pg2.OutsideCount != 1 || pg2.ExcludedCount != 0 || pg2.PendingCount != 1 {
		t.Fatalf("outside 应抢先于 excluded：outside=%d excluded=%d pending=%d",
			pg2.OutsideCount, pg2.ExcludedCount, pg2.PendingCount)
	}

	// 短路序 keep 先于 excluded：黑名单盖住保留者目录时，归类仍是 keep。
	// （保留项在执行器里本来就是硬拒绝，excluded 的说法会夸大黑名单的能力。）
	pg3, err := a.GetPendingFiles(PendingQuery{
		ExcludeDirs: []string{filepath.Join(root, "keepme")},
		SelectedIDs: []uint64{keepID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if pg3.KeepCount != 1 || pg3.ExcludedCount != 0 {
		t.Fatalf("keep 应抢先于 excluded：keep=%d excluded=%d", pg3.KeepCount, pg3.ExcludedCount)
	}
	if pg3.Rows[0].Reason != PendingKeep {
		t.Fatalf("保留项归类失真：%+v", pg3.Rows[0])
	}

	// 未启用黑名单 = 旧行为零变化（ExcludedCount 恒 0，三个冗余项全 pending）。
	pg4, err := a.GetPendingFiles(PendingQuery{SelectedIDs: sel})
	if err != nil {
		t.Fatal(err)
	}
	if pg4.ExcludedCount != 0 || pg4.PendingCount != 3 {
		t.Fatalf("未启用时计数失真：excluded=%d pending=%d，应为 0/3", pg4.ExcludedCount, pg4.PendingCount)
	}

	// 互镜像（清单 ≡ 预览）：同一输入带黑名单参数，两腿逐 id 相等。
	for _, tc := range []struct {
		dirs, exclude []string
	}{
		{nil, []string{insideDir}},
		{[]string{insideDir}, []string{filepath.Join(insideDir, "sub")}},
		{[]string{outsideDir}, []string{insideDir}},
		{nil, []string{"/"}}, // 全吞
	} {
		pv, err := a.PreviewProcessPolicy(tc.dirs, tc.exclude, sel)
		if err != nil {
			t.Fatal(err)
		}
		p, err := a.GetPendingFiles(PendingQuery{
			Dirs: tc.dirs, ExcludeDirs: tc.exclude, SelectedIDs: sel, PageSize: 500,
		})
		if err != nil {
			t.Fatal(err)
		}
		pend, _ := pendingRowsByFlag(p.Rows)
		if !reflect.DeepEqual(sortedUint64(pend), sortedUint64(pv.EffectiveIDs)) {
			t.Fatalf("两腿判据漂移（dirs=%v exclude=%v）：清单 %v vs 预览 %v",
				tc.dirs, tc.exclude, pend, pv.EffectiveIDs)
		}
		if p.PendingCount != pv.EffectiveCount {
			t.Fatalf("两腿计数漂移（dirs=%v exclude=%v）：%d vs %d",
				tc.dirs, tc.exclude, p.PendingCount, pv.EffectiveCount)
		}
	}
}

// 执行腿：黑名单收窄真实处理范围，账本与实际执行同范围，ops:filtered 报数。
func TestExecuteOperationExcludeDirsFilters(t *testing.T) {
	a, rec, _, insideDir, outsideDir := procFixture(t)
	insideSub := filepath.Join(insideDir, "sub")
	inside := idsInDir(t, a, insideDir)
	subIDs := idsInDir(t, a, insideSub)
	outID, outPath := idInDir(t, a, outsideDir)
	subPath := subIDs.paths[0]
	sel := append(append([]uint64{}, inside.ids...), outID)

	if _, err := a.ExecuteOperation(model.OpRequest{
		Kind: "trash", FileIDs: sel, ExcludeDirs: []string{insideSub},
	}); err != nil {
		t.Fatal(err)
	}
	waitOpsDone(t, rec)

	// 黑名单子树里的 b.bin 一字节不许动；其余两项照常处理。
	if _, err := os.Stat(subPath); err != nil {
		t.Errorf("黑名单内的文件不该被动到: %s err=%v", subPath, err)
	}
	for _, p := range inside.paths {
		if p == subPath {
			continue
		}
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("非命中项应被处理: %s err=%v", p, err)
		}
	}
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Errorf("未启用白名单时目录外文件应照常处理: %s", outPath)
	}

	// 账本范围 == 实际执行范围（回撤所见必须与实际相符，同 journal-scope 教训）。
	ops_, err := a.hist.ListOps()
	if err != nil || len(ops_) != 1 {
		t.Fatalf("操作记录 = %+v err=%v", ops_, err)
	}
	_, items, err := a.hist.GetOp(ops_[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("账本条目数 = %d, want 2（黑名单内那条不许入账）", len(items))
	}
	for _, it := range items {
		if it.OrigPath == subPath {
			t.Fatalf("账本记录了黑名单内的文件: %s", it.OrigPath)
		}
	}

	// ops:filtered 必须反映黑名单收窄（前端靠它解释"已选 3 / 实际 2"）。
	payload, ok := rec.lastWith("ops:filtered")
	if !ok {
		t.Fatalf("黑名单生效时应发 ops:filtered，实收 %v", rec.names())
	}
	m := payload.(map[string]any)
	if m["matched"] != 2 || m["selected"] != 3 {
		t.Errorf("ops:filtered = %v，应 matched=2 selected=3", m)
	}
}

// 白名单 + 黑名单同时生效：a 被两侧放行、b 被黑名单拦、c 被白名单拦。
func TestExecuteOperationExcludeWithProcessDirs(t *testing.T) {
	a, rec, _, insideDir, outsideDir := procFixture(t)
	insideSub := filepath.Join(insideDir, "sub")
	inside := idsInDir(t, a, insideDir)
	subIDs := idsInDir(t, a, insideSub)
	outID, outPath := idInDir(t, a, outsideDir)
	sel := append(append([]uint64{}, inside.ids...), outID)

	if _, err := a.ExecuteOperation(model.OpRequest{
		Kind: "trash", FileIDs: sel,
		ProcessDirs: []string{insideDir}, ExcludeDirs: []string{insideSub},
	}); err != nil {
		t.Fatal(err)
	}
	waitOpsDone(t, rec)

	for i, p := range inside.paths {
		if p == subIDs.paths[0] {
			if _, err := os.Stat(p); err != nil {
				t.Errorf("黑名单内项不该被动: %s err=%v", p, err)
			}
			continue
		}
		_ = i
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("白名单内非命中项应被处理: %s", p)
		}
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Errorf("白名单外文件不该被动到: %s err=%v", outPath, err)
	}
}

// 全部勾选都被黑名单吞掉：必须明确拒绝（不静默执行 0 项），
// 且拒绝后 opsRunning 复位、没有任何文件被改动。
func TestExecuteOperationExcludeDirsRejectsAll(t *testing.T) {
	a, rec, _, insideDir, _ := procFixture(t)
	subIDs := idsInDir(t, a, filepath.Join(insideDir, "sub"))
	subPath := subIDs.paths[0]

	_, err := a.ExecuteOperation(model.OpRequest{
		Kind: "trash", FileIDs: subIDs.ids, ExcludeDirs: []string{insideDir},
	})
	if err == nil {
		t.Fatal("全部落空必须返回错误，不能静默执行 0 项（用户会以为做了）")
	}
	if !strings.Contains(err.Error(), "取消") {
		t.Errorf("拒绝文案应明说已取消：%q", err)
	}
	if _, serr := os.Stat(subPath); serr != nil {
		t.Fatalf("被拒绝的操作不该改动任何文件: %s err=%v", subPath, serr)
	}
	a.mu.Lock()
	running := a.opsRunning
	a.mu.Unlock()
	if running {
		t.Fatal("拒绝后 opsRunning 未复位——应用会永久卡在「操作执行中」")
	}

	// 反证复位有效：紧接着发起一次合法操作应成功。
	other := idsInDir(t, a, insideDir)
	legal := []uint64{}
	for i, id := range other.ids {
		if other.paths[i] != subPath {
			legal = append(legal, id)
		}
	}
	if _, err := a.ExecuteOperation(model.OpRequest{Kind: "trash", FileIDs: legal}); err != nil {
		t.Fatalf("拒绝后应能正常发起后续操作: %v", err)
	}
	waitOpsDone(t, rec)
}

// 结构性边界：黑名单是拟处理集的投影，不是第二套保留/清理机制。
// 即便预览/清单被"全吞"级黑名单覆盖，结果集成员与保留标记必须分毫不动——
// 哪天有人把 excludeDirs 误接进保留引擎或结果集清理，这条必红。
func TestExcludeDirsNeverTouchesKeepOrResultView(t *testing.T) {
	a, _, _, _, _ := procFixture(t)
	before := allGroups(t, a)

	if _, err := a.PreviewProcessPolicy(nil, []string{"/"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.GetPendingFiles(PendingQuery{ExcludeDirs: []string{"/"}}); err != nil {
		t.Fatal(err)
	}

	after := allGroups(t, a)
	if len(after) != len(before) {
		t.Fatalf("组数变了：%d → %d", len(before), len(after))
	}
	for bi, g := range before {
		ag := after[bi]
		if len(ag.Files) != len(g.Files) {
			t.Fatalf("组 %d 成员数变了", g.GroupID)
		}
		for fi, f := range g.Files {
			if ag.Files[fi].IsKeep != f.IsKeep {
				t.Fatalf("保留标记被黑名单改写了：%s %v→%v", f.Path, f.IsKeep, ag.Files[fi].IsKeep)
			}
		}
	}
}
