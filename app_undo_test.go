package main

// v0.5.0 功能 4（Task 9）：清理操作接入写前日志。

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

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
	if meta.Kind != "hardlink" || meta.Done != 1 || meta.Reclaimable == 0 {
		t.Fatalf("meta = %+v", meta)
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

// 历史库缺失时清理照常执行（journal no-op），不得阻塞主流程。
func TestExecuteOperationWithoutHistoryStillWorks(t *testing.T) {
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

	if _, err := a.ExecuteOperation(model.OpRequest{Kind: "trash", FileIDs: sel}); err != nil {
		t.Fatal(err)
	}
	waitOpsDone(t, rec)
	a.mu.Lock()
	n := len(a.groups)
	a.mu.Unlock()
	if n != 0 { // S8 语义下结果集照常裁剪
		t.Fatalf("结果集应清空, got %d 组", n)
	}
}
