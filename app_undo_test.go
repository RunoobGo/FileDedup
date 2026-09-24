package main

// v0.5.0 功能 4（Task 9）：清理操作接入写前日志。

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"filededup/internal/history"
	"filededup/internal/model"
)

// waitOpsDone 阻塞直到 ops:done（超时由 eventRecorder 内部兜底）。
func waitOpsDone(t *testing.T, rec *eventRecorder) {
	t.Helper()
	if ev := rec.waitTerminal(t, "ops"); ev != "ops:done" {
		t.Fatalf("ops 终止事件 = %s", ev)
	}
}

// 非保留项 id（依据结果视图的 isKeep 建议）。
func redundantIDs(t *testing.T, a *App) []uint64 {
	t.Helper()
	r, err := a.GetResultGroups(ResultQuery{PageSize: 500})
	if err != nil {
		t.Fatal(err)
	}
	var ids []uint64
	for _, g := range r.Groups {
		for _, f := range g.Files {
			if !f.IsKeep {
				ids = append(ids, f.ID)
			}
		}
	}
	return ids
}

// trash 对已消失文件（S8 Skipped）：账本仍完整落盘——计划、逐条收口、
// Finalize（无 planned 残留）、done 计数为 0、平台 undoable。
func TestOpJournalTrashSkippedItems(t *testing.T) {
	a, rec := newHistApp(t)
	g := mkHistGroup(1, 1000, "/u/a/x.bin", "/u/b/y.bin", "/u/c/z.bin")
	id, err := a.hist.SaveScan(model.ScanConfig{Roots: []string{"/u"}},
		[]*model.DuplicateGroup{g}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.LoadScanHistory(id); err != nil {
		t.Fatal(err)
	}
	var sel []uint64
	a.mu.Lock()
	for _, f := range a.groups[0].Files {
		sel = append(sel, f.ID)
	}
	a.mu.Unlock()

	if _, err := a.ExecuteOperation(model.OpRequest{Kind: "trash", FileIDs: sel}); err != nil {
		t.Fatal(err)
	}
	waitOpsDone(t, rec)

	ops, err := a.hist.ListOps()
	if err != nil || len(ops) != 1 {
		t.Fatalf("应有 1 条操作记录: %+v err=%v", ops, err)
	}
	meta := ops[0]
	wantUndoable := runtime.GOOS != "windows"
	if meta.Kind != "trash" || meta.Items != 3 || meta.Done != 0 || meta.Undoable != wantUndoable {
		t.Fatalf("meta = %+v（undoable 平台值 %v）", meta, wantUndoable)
	}
	if meta.HistID != id {
		t.Fatalf("hist_id 未关联当前历史: %+v", meta)
	}
	_, items, err := a.hist.GetOp(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.State != "skipped" { // 无 planned/cancelled 残留 = Finalize 生效
			t.Fatalf("条目 = %+v，期望全部 skipped", it)
		}
	}
}

// hardlink 真实文件：done 条目必须记录 link_src（回撤据此重建内容校验）。
func TestOpJournalHardlinkRecordsLinkSrc(t *testing.T) {
	root := t.TempDir()
	payload := []byte("JOURNAL-HARDLINK-PAYLOAD")
	if err := os.WriteFile(filepath.Join(root, "a.bin"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "longer_name.bin"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	a, rec := newHistApp(t)
	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}, Threads: 2}); err != nil {
		t.Fatal(err)
	}
	if ev := rec.waitTerminal(t, "scan"); ev != "scan:done" {
		t.Fatalf("scan 终止事件 = %s", ev)
	}
	sel := redundantIDs(t, a)
	if len(sel) != 1 {
		t.Fatalf("应有 1 个冗余项, got %v", sel)
	}
	if _, err := a.ExecuteOperation(model.OpRequest{Kind: "hardlink", FileIDs: sel}); err != nil {
		t.Fatal(err)
	}
	waitOpsDone(t, rec)

	ops, err := a.hist.ListOps()
	if err != nil || len(ops) != 1 {
		t.Fatalf("操作记录 = %+v err=%v", ops, err)
	}
	meta, items, err := a.hist.GetOp(ops[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	// B6（R-操作-3）：硬链接合并当期不释放磁盘（数据块转为共享），执行器把字节
	// 记入 LinkedBytes 而非 Reclaimed，历史「回收空间」因此为 0 —— 这是修正后的
	// 真话；修正前此处按 SUM(size) 记出非零假账，故断言从「!=0」翻正为「==0」。
	if meta.Kind != "hardlink" || meta.Done != 1 || meta.Reclaimable != 0 {
		t.Fatalf("meta = %+v（硬链接回收空间应为 0）", meta)
	}
	if len(items) != 1 || items[0].State != "done" || items[0].LinkSrc == "" {
		t.Fatalf("items = %+v，期望 done 且记录 link_src", items)
	}
	if st, err := os.Stat(items[0].LinkSrc); err != nil || !st.Mode().IsRegular() {
		t.Fatalf("link_src 指向失效: %v", err)
	}
}

// 永久删除：账本照记但 undoable=0（前端据此隐藏回撤、给系统回收站引导）。
func TestOpJournalDeleteNotUndoable(t *testing.T) {
	root := t.TempDir()
	payload := []byte("JOURNAL-DELETE-PAYLOAD")
	if err := os.WriteFile(filepath.Join(root, "a.bin"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.bin"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	a, rec := newHistApp(t)
	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}, Threads: 2}); err != nil {
		t.Fatal(err)
	}
	if ev := rec.waitTerminal(t, "scan"); ev != "scan:done" {
		t.Fatalf("scan 终止事件 = %s", ev)
	}
	sel := redundantIDs(t, a)
	if _, err := a.ExecuteOperation(model.OpRequest{
		Kind: "delete", FileIDs: sel, ConfirmDanger: true,
	}); err != nil {
		t.Fatal(err)
	}
	waitOpsDone(t, rec)

	ops, err := a.hist.ListOps()
	if err != nil || len(ops) != 1 {
		t.Fatalf("操作记录 = %+v err=%v", ops, err)
	}
	if ops[0].Kind != "delete" || ops[0].Undoable {
		t.Fatalf("delete 必须不可回撤: %+v", ops[0])
	}
	if ops[0].Done != len(sel) {
		t.Fatalf("done 计数 = %d, want %d", ops[0].Done, len(sel))
	}
}

// contains 判断是否收到过某事件（emit 桩记录的名称列表）。
func (e *eventRecorder) contains(name string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, ev := range e.events {
		if ev == name {
			return true
		}
	}
	return false
}

// 历史库缺失时的清理行为（2026-09-18 审查 C3，用户裁定「仅可回撤类型拒绝」）：
//   - 承诺可回撤的（非 Windows 的回收站/移动/硬链接）必须拒绝执行——无账本即无法回撤，
//     原先只发一条事件便照常移动文件，用户按手册预期可撤而实际不能；
//   - 本就不承诺回撤的（永久删除，以及 Windows 回收站）照常执行，但必须显式留痕
//     （app:error），绝不静默——一并拒绝会把用户逼向唯一可用的破坏性路径。
//
// 两类划分与 app.go beginJournal 的 undoable 判定同源，含 runtime.GOOS 分支。
func TestExecuteOperationWithoutHistory(t *testing.T) {
	newLoadedApp := func(t *testing.T) (*App, *eventRecorder, []uint64) {
		t.Helper()
		a, rec := newHistApp(t)
		g := mkHistGroup(1, 1000, "/u/a/x.bin", "/u/b/y.bin")
		id, err := a.hist.SaveScan(model.ScanConfig{Roots: []string{"/u"}},
			[]*model.DuplicateGroup{g}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.LoadScanHistory(id); err != nil {
			t.Fatal(err)
		}
		a.mu.Lock()
		sel := []uint64{a.groups[0].Files[0].ID, a.groups[0].Files[1].ID}
		a.hist = nil // 模拟历史库不可用
		a.mu.Unlock()
		return a, rec, sel
	}

	t.Run("可回撤类拒绝且不动文件", func(t *testing.T) {
		kinds := []string{"trash", "move", "hardlink"}
		if runtime.GOOS == "windows" {
			// trash 在 Windows 属"本就不承诺回撤"类（回收站拿不到原路返回映射），
			// 走下一个子测试的留痕放行断言。
			kinds = []string{"move", "hardlink"}
		}
		for _, kind := range kinds {
			a, rec, sel := newLoadedApp(t)
			target := t.TempDir()
			a.authorizeDir(target) // 排除 H5 目标未授权这一干扰原因
			op := model.OpRequest{Kind: kind, FileIDs: sel, TargetDir: target}
			if _, err := a.ExecuteOperation(op); err == nil {
				t.Fatalf("%s：账本不可用时不得受理（将造成无记录的文件移动）", kind)
			}
			a.mu.Lock()
			running, groups := a.opsRunning, len(a.groups)
			a.mu.Unlock()
			if running {
				t.Fatalf("%s：拒绝后 opsRunning 未复位，此后所有清理都会被拒", kind)
			}
			if groups != 1 {
				t.Fatalf("%s：拒绝后结果集不应被裁剪", kind)
			}
			if rec.contains("ops:done") {
				t.Fatalf("%s：拒绝却发出了 ops:done", kind)
			}
		}
	})

	t.Run("不可回撤类放行但显式留痕", func(t *testing.T) {
		// delete 三平台都不可回撤；trash 只在 Windows 不可回撤（同一 undoable 判定）。
		kinds := []string{"delete"}
		if runtime.GOOS == "windows" {
			kinds = append(kinds, "trash")
		}
		for _, kind := range kinds {
			a, rec, sel := newLoadedApp(t)
			op := model.OpRequest{Kind: kind, FileIDs: sel}
			if kind == "delete" {
				op.ConfirmDanger = true
			} else {
				op.TargetDir = t.TempDir()
				a.authorizeDir(op.TargetDir)
			}
			if _, err := a.ExecuteOperation(op); err != nil {
				t.Fatalf("%s：本就不支持回撤，账本故障不应使其不可用: %v", kind, err)
			}
			// app:error 由 beginJournal 在派生执行 goroutine 之前同步发出，
			// 因此这里无需等待操作收尾即可断言（本机无 Windows runner，
			// trash 的实际执行结果不作断言——留痕才是本用例的靶心）。
			if !rec.contains("app:error") {
				t.Fatalf("%s：未写入账本必须显式告警（app:error），不得静默放行", kind)
			}
			if kind != "delete" {
				continue
			}
			waitOpsDone(t, rec)
			a.mu.Lock()
			n := len(a.groups)
			a.mu.Unlock()
			if n != 0 { // S8 语义下结果集照常裁剪
				t.Fatalf("%s：结果集应清空, got %d 组", kind, n)
			}
		}
	})
}

// ---------- Task 11：App.UndoOperation ----------

// waitUndoDone 阻塞直到 ops:undo:done。
func waitUndoDone(t *testing.T, rec *eventRecorder) {
	t.Helper()
	if ev := rec.waitTerminal(t, "undo"); ev != "ops:undo:done" {
		t.Fatalf("undo 终止事件 = %s", ev)
	}
}

// seedDoneOp 直接落一条「已执行完」的操作账本（绕过真实清理，构造回撤输入）。
// dests[i] 为空串模拟回收站映射缺失；hash 仅 hardlink 类型用到。
func seedDoneOp(t *testing.T, a *App, kind string, undoable bool,
	origs, dests []string, size uint64) int64 {
	t.Helper()
	plans := make([]history.OpItemPlan, len(origs))
	for i, p := range origs {
		plans[i] = history.OpItemPlan{OrigPath: p, Size: size}
	}
	opID, err := a.hist.BeginOp(kind, "", 0, undoable, plans)
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range origs {
		if err := a.hist.FinishItem(opID, p, dests[i], "", history.StateDone, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.hist.FinalizeOp(opID, 0); err != nil {
		t.Fatal(err)
	}
	return opID
}

// trash 记录回撤：文件回原位、条目变 undone、重复回撤幂等（0 项处理）。
func TestUndoOperationTrashRestores(t *testing.T) {
	dir := t.TempDir()
	content := []byte("UNDO-APP-TRASH!!!")
	dest := filepath.Join(dir, "trash", "a.bin")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, content, 0o644); err != nil {
		t.Fatal(err)
	}
	orig := filepath.Join(dir, "home", "a.bin")
	a, rec := newHistApp(t)
	opID := seedDoneOp(t, a, "trash", true, []string{orig}, []string{dest}, uint64(len(content)))

	if _, err := a.UndoOperation(opID); err != nil {
		t.Fatal(err)
	}
	waitUndoDone(t, rec)
	if got, err := os.ReadFile(orig); err != nil || string(got) != string(content) {
		t.Fatalf("文件未回原位: err=%v", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("回收站侧残留")
	}
	_, items, err := a.hist.GetOp(opID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].State != history.StateUndone {
		t.Fatalf("条目未记 undone: %+v", items)
	}

	// 幂等：再次回撤 → 正常收尾但 0 项处理
	if _, err := a.UndoOperation(opID); err != nil {
		t.Fatal(err)
	}
	waitUndoDone(t, rec)
	if got, _ := os.ReadFile(orig); string(got) != string(content) {
		t.Fatal("重复回撤改动了已恢复的文件")
	}
}

// 不可撤记录（delete / Windows trash 映射缺失）→ 入口同步报错。
// 2026-09-19：文案改为「按 kind 分别解释原因 + 给出可执行下一步」，
// 故断言不再匹配旧统一措辞，改为匹配 delete 专属说明。
// M79（2026-09-22）：入口下发的是**原因码**，中文改由前端查表 ⇒ 这里改比码。
// 「报的是哪一类原因」这条判据一字未丢，只是值域从中文正文换成码；
// 那两句中文的判据在 `frontend/tests/undo-reason.test.ts`（逐字钉）。
func TestUndoOperationNotUndoable(t *testing.T) {
	a, _ := newHistApp(t)
	opID := seedDoneOp(t, a, "delete", false, []string{"/nn/x.bin"}, []string{""}, 3)
	if _, err := a.UndoOperation(opID); err == nil ||
		!strings.Contains(err.Error(), undoCodePermanentDelete) {
		t.Fatalf("delete 记录应拒绝回撤: %v", err)
	}
	if _, err := a.UndoOperation(99999); err == nil ||
		!strings.Contains(err.Error(), "不存在") {
		t.Fatalf("缺失记录应报错: %v", err)
	}
}

// 部分条目失败不中断整批：映射缺失项记 undo_failed，其余照常恢复。
func TestUndoOperationPartialFailure(t *testing.T) {
	dir := t.TempDir()
	content := []byte("PARTIAL-OK!")
	dest := filepath.Join(dir, "t", "b.bin")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, content, 0o644); err != nil {
		t.Fatal(err)
	}
	a, rec := newHistApp(t)
	opID := seedDoneOp(t, a, "trash", true,
		[]string{filepath.Join(dir, "lost", "a.bin"), filepath.Join(dir, "home", "b.bin")},
		[]string{"", dest}, uint64(len(content)))

	if _, err := a.UndoOperation(opID); err != nil {
		t.Fatal(err)
	}
	waitUndoDone(t, rec)
	_, items, err := a.hist.GetOp(opID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].State != history.StateUndoFailed ||
		items[1].State != history.StateUndone {
		t.Fatalf("部分失败收口不符: %+v", items)
	}
	if items[0].Err == "" {
		t.Fatal("失败原因未落库")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "home", "b.bin")); string(got) != string(content) {
		t.Fatal("正常项未恢复")
	}
}

// 历史库缺失 / 操作在途 → 同步拒绝。
func TestUndoOperationGates(t *testing.T) {
	a, _ := newHistApp(t)
	a.mu.Lock()
	a.hist = nil
	a.mu.Unlock()
	if _, err := a.UndoOperation(1); err == nil || !strings.Contains(err.Error(), "历史库不可用") {
		t.Fatalf("hist=nil 应报错: %v", err)
	}

	a2, _ := newHistApp(t)
	a2.mu.Lock()
	a2.opsRunning = true
	a2.mu.Unlock()
	if _, err := a2.UndoOperation(1); err == nil {
		t.Fatal("操作在途时应拒绝")
	}
}

// ---------- 单项回撤（在全部回撤之上的增量通道） ----------

// 单项回撤只处理目标项：其余 done 项不受牵连；已 undone 项再撤同步拒绝。
func TestUndoOperationItemSingleRestore(t *testing.T) {
	dir := t.TempDir()
	content := []byte("UNDO-ITEM-OK!!")
	dest := filepath.Join(dir, "trash", "b.bin")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, content, 0o644); err != nil {
		t.Fatal(err)
	}
	a, rec := newHistApp(t)
	orig := filepath.Join(dir, "home", "b.bin")
	opID := seedDoneOp(t, a, "trash", true,
		[]string{filepath.Join(dir, "lost", "a.bin"), orig},
		[]string{"", dest}, uint64(len(content)))
	_, items, err := a.hist.GetOp(opID)
	if err != nil {
		t.Fatal(err)
	}
	goodID := items[1].ID

	if _, err := a.UndoOperationItem(opID, goodID); err != nil {
		t.Fatal(err)
	}
	waitUndoDone(t, rec)
	if got, err := os.ReadFile(orig); err != nil || string(got) != string(content) {
		t.Fatalf("目标项未回原位: err=%v", err)
	}
	_, items, err = a.hist.GetOp(opID)
	if err != nil {
		t.Fatal(err)
	}
	if items[0].State != history.StateDone {
		t.Fatalf("非目标项被牵连: %+v", items[0])
	}
	if items[1].State != history.StateUndone {
		t.Fatalf("目标项未记 undone: %+v", items[1])
	}
	// 已回撤项再撤 → 同步拒绝（done/undo_failed 之外不可撤）
	if _, err := a.UndoOperationItem(opID, goodID); err == nil ||
		!strings.Contains(err.Error(), "不可回撤") {
		t.Fatalf("undone 项应拒绝再撤: %v", err)
	}
}

// 回收站映射缺失项：单项回撤走异步逐项失败——undo_failed + 原因落库，可修正后重试。
func TestUndoOperationItemMissingDestFails(t *testing.T) {
	a, rec := newHistApp(t)
	opID := seedDoneOp(t, a, "trash", true, []string{"/nn/x.bin"}, []string{""}, 3)
	_, items, err := a.hist.GetOp(opID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.UndoOperationItem(opID, items[0].ID); err != nil {
		t.Fatal(err)
	}
	waitUndoDone(t, rec)
	_, items, err = a.hist.GetOp(opID)
	if err != nil {
		t.Fatal(err)
	}
	if items[0].State != history.StateUndoFailed || items[0].Err == "" {
		t.Fatalf("映射缺失项应记 undo_failed+原因: %+v", items[0])
	}
}

// 单项出口的三道门禁（原注释「记录不可撤 / 条目不属于该记录」的展开）：
// 不可撤 / 记录不存在 / 非 done·undo_failed 态各自同步拒绝。
// M79（2026-09-22）：第一道改比**原因码**（同 `TestUndoOperationNotUndoable` 的注释）；
// 第二道比的「不存在」、第三道（在途，只断言被拒、不比串）都不出自那两句被搬家的中文 ⇒ 一字未动，
// 也别把它们算进搬家对账里。同族还有一处「不可回撤」在 `TestUndoOperationItemSingleRestore`，
// 钉的是 `app.go:2326` 那句**按状态**的拒绝，同理不计入。
func TestUndoOperationItemGuards(t *testing.T) {
	a, _ := newHistApp(t)
	opID := seedDoneOp(t, a, "delete", false, []string{"/nn/x.bin"}, []string{""}, 3)
	_, items, err := a.hist.GetOp(opID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.UndoOperationItem(opID, items[0].ID); err == nil ||
		!strings.Contains(err.Error(), undoCodePermanentDelete) {
		t.Fatalf("delete 记录应拒绝回撤: %v", err)
	}
	trashOp := seedDoneOp(t, a, "trash", true,
		[]string{filepath.Join(t.TempDir(), "h", "a.bin")}, []string{""}, 3)
	if _, err := a.UndoOperationItem(trashOp, 999999); err == nil ||
		!strings.Contains(err.Error(), "不存在") {
		t.Fatalf("缺失条目应报错: %v", err)
	}
	// 操作在途 → 同步拒绝
	a.mu.Lock()
	a.opsRunning = true
	a.mu.Unlock()
	_, items, _ = a.hist.GetOp(trashOp)
	if _, err := a.UndoOperationItem(trashOp, items[0].ID); err == nil {
		t.Fatal("操作在途时应拒绝")
	}
}

// TestUndoReasonCodeExplainsWhichBranch 是 TestUndoableReasonExplainsAndGivesNextStep
// 在 M79（裁定「判据归后端、文案归前端」）之后的对应物。逐格对账如下，一条判据没丢：
//
//	原格	                              → 现在的归属
//	:535 delete 文案含「永久删除/磁盘移除/移入回收站」 → node `undo-reason.test.ts` 的 toast 体逐字格
//	                                     （逐字相等严格强于「含这三个词」）
//	:541 不得回退成旧笼统措辞            → 同上（逐字相等 ⇒ 不可能等于旧句）+ Go 侧「必须是那两个码之一」
//	:551 windows trash 含「映射/打开系统回收站/还原」 → node 侧同一族的 title/toast 逐字格
//	:565 非 Windows 不得给 Windows 引导   → 本函数 goos=linux/darwin 的码互异格
//	:568 非 Windows 的 trash 走兜底       → 本函数 trash@linux/darwin == 永久删除码
//	:573 未知 kind 不得空                → 本函数「码非空且落在契约两码内」格
//
// ★ 搬家换来的一处净增益：改前 :547/:559 两格按 `runtime.GOOS` 分流、各自在非目标机器上
//
//	`t.Skip`（Windows 那一格在本机从没真跑过）。现在码由 `undoReasonCodeFor(kind, goos)`
//	算，平台真值进参数 ⇒ 四格在任何一台机器上都真跑，本函数一条 Skip 都不留。
//
// 2026-09-19 那段用户报障（「移入回收站的操作显示为不可回撤」）要的东西没有作废：
// 「讲明白原因与出路」仍然必须有，只是拆成两半——后端负责**分成两类**（两个码），
// 前端负责**说人话**（`frontend/src/utils/undoReason.ts` 是全仓唯一写那两句中文的地方）。
func TestUndoReasonCodeExplainsWhichBranch(t *testing.T) {
	t.Run("delete_出永久删除码", func(t *testing.T) {
		if got := undoReasonCode("delete"); got != undoCodePermanentDelete {
			t.Fatalf("delete 应出永久删除码，实为 %q", got)
		}
	})

	t.Run("windows_trash_出回收站码", func(t *testing.T) {
		if got := undoReasonCodeFor("trash", "windows"); got != undoCodeWindowsTrash {
			t.Fatalf("Windows 上的 trash 应出回收站码，实为 %q", got)
		}
	})

	t.Run("非windows的trash走永久删除码", func(t *testing.T) {
		for _, goos := range []string{"linux", "darwin"} {
			if got := undoReasonCodeFor("trash", goos); got != undoCodePermanentDelete {
				t.Fatalf("%s 上的 trash 拿到了 Windows 专属那一码：%q", goos, got)
			}
		}
	})

	t.Run("未知kind也有码且必须是契约内的码", func(t *testing.T) {
		msg := undoReasonCode("some-future-kind")
		if msg == "" {
			t.Fatal("未知 kind 不得返回空码")
		}
		if msg != undoCodePermanentDelete {
			t.Fatalf("未知 kind 应兜进永久删除码（与 undoableFor 的取值一致），实为 %q", msg)
		}
	})

	// 码字面量本身是**前后端契约**：前端 undoReason.ts 按这两个字符串查文案，
	// 改一个字节就是"后端发了个前端不认的码"，用户看到的说明退化成透传的裸码。
	// 这一格钉的是常量，不是算法 ⇒ 算法全对但常量被手滑改写了也红。
	t.Run("码字面量与前端契约逐字相同", func(t *testing.T) {
		if undoCodeWindowsTrash != "undo-code-windows-trash" {
			t.Fatalf("回收站码字面量漂移：%q", undoCodeWindowsTrash)
		}
		if undoCodePermanentDelete != "undo-code-permanent-delete" {
			t.Fatalf("永久删除码字面量漂移：%q", undoCodePermanentDelete)
		}
	})
}

// TestUndoableMatchesKindAndPlatform 钉死 undoable 判定与文案分流的对应关系，
// 防止「判定说可撤、文案说不可撤」这类自相矛盾。
// ★ APP-6（2026-09-21 全量审查）：本用例原先自带一份 `undoableOf` 内联判据，
// 于是"生产改了、测试没改"这类漂移它自己永远看不见——第三条判据就在断言里。
// 现在改问生产那一份 undoableFor，断言内容一字未动。
func TestUndoableMatchesKindAndPlatform(t *testing.T) {
	undoableOf := func(kind string) bool {
		return undoableFor(kind, runtime.GOOS)
	}
	if undoableOf("delete") {
		t.Fatal("delete 必须不可回撤")
	}
	// trash 的回撤能力按平台：Windows 不可撤（无落点映射），其余可撤
	if runtime.GOOS == "windows" {
		if undoableOf("trash") {
			t.Fatal("Windows 上 trash 必须不可回撤")
		}
	} else if !undoableOf("trash") {
		t.Fatal("非 Windows 上 trash 应当可回撤")
	}
	for _, k := range []string{"move", "hardlink"} {
		if !undoableOf(k) {
			t.Fatalf("%s 应当可回撤", k)
		}
	}
}
