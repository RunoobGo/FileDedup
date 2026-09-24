package history

// 2026-09-18 审查 I6：回撤写前落账的状态机与聚合口径。

import (
	"path/filepath"
	"testing"
)

// seedExecutedOp 落一条「三项都执行成功」的账（planned→done→Finalize）。
// 用 delete 类型：它是真正释放磁盘的操作，执行器算出的 reclaimed = 三项之和 6000，
// 与本 helper 传给 FinalizeOp 的值一致（B6 后 reclaimed 由调用方传入、库内不重算）。
func seedExecutedOp(t *testing.T, s *Store) int64 {
	t.Helper()
	opID, err := s.BeginOp("delete", "", 0, true, opPlans())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range opPlans() {
		if err := s.FinishItem(opID, p.OrigPath, "/trash/"+filepath.Base(p.OrigPath), "", StateDone, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.FinalizeOp(opID, 6000); err != nil {
		t.Fatal(err)
	}
	return opID
}

// undoing 是执行中的瞬时态：不得从聚合里凭空掉出来（Done 含 undoing），
// 也不得计入 Undone（还没撤成）。
func TestUndoingCountedAsExecuted(t *testing.T) {
	s := testDB(t)
	opID := seedExecutedOp(t, s)
	_, items, err := s.GetOp(opID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkItemUndo(items[0].ID, StateUndoing, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkItemUndo(items[1].ID, StateUndone, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkItemUndo(items[2].ID, StateUndoFailed, "外部占用"); err != nil {
		t.Fatal(err)
	}

	meta, _, err := s.GetOp(opID)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Items != 3 || meta.Done != 3 || meta.Undone != 1 || meta.Failed != 0 {
		t.Fatalf("聚合不符（undoing 应计入已执行）: %+v", meta)
	}
	if meta.Reclaimable != 6000 {
		t.Fatalf("reclaimable = %d, want 6000", meta.Reclaimable)
	}
	// 列表口径与明细一致
	list, err := s.ListOps()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Done != 3 || list[0].Undone != 1 {
		t.Fatalf("ListOps 聚合不符: %+v", list)
	}
}

// 进程死于「已标 undoing、结果未落账」之间：重启后收口为 undo_failed（可重试），
// 且不得误伤已收口的 done / undone 条目。
func TestUndoingSweptOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	opID := seedExecutedOp(t, s)
	_, items, err := s.GetOp(opID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkItemUndo(items[0].ID, StateUndoing, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkItemUndo(items[1].ID, StateUndone, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil { // 模拟进程退出
		t.Fatal(err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	_, items, err = s2.GetOp(opID)
	if err != nil {
		t.Fatal(err)
	}
	got := itemState(t, items, "/op/a.bin")
	if got.State != StateUndoFailed || got.Err == "" {
		t.Fatalf("undoing 残留应收口为带原因的 undo_failed: %+v", got)
	}
	if it := itemState(t, items, "/op/b.bin"); it.State != StateUndone {
		t.Fatalf("已回撤条目被误伤: %+v", it)
	}
	if it := itemState(t, items, "/op/c.bin"); it.State != StateDone || it.Err != "" {
		t.Fatalf("未参与回撤的条目被误伤: %+v", it)
	}
}
