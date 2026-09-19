package history

import (
	"path/filepath"
	"testing"
)

func opPlans() []OpItemPlan {
	return []OpItemPlan{
		{OrigPath: "/op/a.bin", Hash: [32]byte{1}, Size: 1000, MtimeNs: 11},
		{OrigPath: "/op/b.bin", Hash: [32]byte{2}, Size: 2000, MtimeNs: 22},
		{OrigPath: "/op/c.bin", Hash: [32]byte{3}, Size: 3000, MtimeNs: 33},
	}
}

func itemState(t *testing.T, items []OpItem, path string) OpItem {
	t.Helper()
	for _, it := range items {
		if it.OrigPath == path {
			return it
		}
	}
	t.Fatalf("条目 %s 不存在", path)
	return OpItem{}
}

// BeginOp 写前落盘：全部条目初始为 planned。
func TestBeginOpAllPlanned(t *testing.T) {
	s := testDB(t)
	opID, err := s.BeginOp("trash", "", 7, true, opPlans())
	if err != nil {
		t.Fatal(err)
	}
	if opID == 0 {
		t.Fatal("BeginOp 返回 0")
	}
	meta, items, err := s.GetOp(opID)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Kind != "trash" || meta.HistID != 7 || !meta.Undoable {
		t.Fatalf("meta = %+v", meta)
	}
	if len(items) != 3 {
		t.Fatalf("items = %d", len(items))
	}
	for _, it := range items {
		if it.State != StatePlanned {
			t.Fatalf("%s state = %s, 期望 planned", it.OrigPath, it.State)
		}
	}
	if it := itemState(t, items, "/op/b.bin"); it.Size != 2000 || it.MtimeNs != 22 || it.Hash != ([32]byte{2}) {
		t.Fatalf("b.bin 回读不符: %+v", it)
	}
}

// FinishItem 逐条收口；FinalizeOp 汇总 done 计数与 reclaimed 字节。
func TestFinishAndFinalize(t *testing.T) {
	s := testDB(t)
	opID, err := s.BeginOp("move", "/dst", 0, true, opPlans())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishItem(opID, "/op/a.bin", "/dst/a.bin", "", StateDone, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishItem(opID, "/op/b.bin", "", "", StateFailed, "permission denied"); err != nil {
		t.Fatal(err)
	}
	// c.bin 未处理，Finalize 时应转 cancelled
	if err := s.FinalizeOp(opID); err != nil {
		t.Fatal(err)
	}
	meta, items, err := s.GetOp(opID)
	if err != nil {
		t.Fatal(err)
	}
	if got := itemState(t, items, "/op/a.bin"); got.State != StateDone || got.DestPath != "/dst/a.bin" {
		t.Fatalf("a.bin = %+v", got)
	}
	if got := itemState(t, items, "/op/b.bin"); got.State != StateFailed || got.Err != "permission denied" {
		t.Fatalf("b.bin = %+v", got)
	}
	if got := itemState(t, items, "/op/c.bin"); got.State != StateCancelled {
		t.Fatalf("c.bin = %+v, 期望 cancelled", got)
	}
	if meta.Done != 1 || meta.Failed != 1 || meta.Items != 3 {
		t.Fatalf("聚合计数 = %+v", meta)
	}
	if meta.Reclaimable != 1000 {
		t.Fatalf("Reclaimable = %d, 期望 1000（仅 done 计）", meta.Reclaimable)
	}
}

func TestFinishItemUnknownPath(t *testing.T) {
	s := testDB(t)
	opID, _ := s.BeginOp("trash", "", 0, true, opPlans())
	if err := s.FinishItem(opID, "/nope.bin", "", "", StateDone, ""); err == nil {
		t.Fatal("未登记路径应报错")
	}
}

// MarkItemUndo：成功置 undone，失败置 undo_failed 且保留原 dest。
func TestMarkItemUndo(t *testing.T) {
	s := testDB(t)
	opID, _ := s.BeginOp("trash", "", 0, true, opPlans())
	if err := s.FinishItem(opID, "/op/a.bin", "/Trash/a.bin", "", StateDone, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.FinalizeOp(opID); err != nil {
		t.Fatal(err)
	}
	_, items, _ := s.GetOp(opID)
	a := itemState(t, items, "/op/a.bin")

	if err := s.MarkItemUndo(a.ID, StateUndoFailed, "目标位置已被占用"); err != nil {
		t.Fatal(err)
	}
	_, items, _ = s.GetOp(opID)
	a = itemState(t, items, "/op/a.bin")
	if a.State != StateUndoFailed || a.Err != "目标位置已被占用" || a.DestPath != "/Trash/a.bin" {
		t.Fatalf("undo_failed 后 = %+v", a)
	}

	if err := s.MarkItemUndo(a.ID, StateUndone, ""); err != nil {
		t.Fatal(err)
	}
	meta, items, _ := s.GetOp(opID)
	a = itemState(t, items, "/op/a.bin")
	if a.State != StateUndone || a.Err != "" {
		t.Fatalf("undone 后 = %+v", a)
	}
	if meta.Undone != 1 || meta.Done != 1 {
		t.Fatalf("meta = %+v（回撤不冲销历史记录）", meta)
	}
}

// 进程崩溃残留：重启后 planned 统一收口为 interrupted。
func TestStartupInterruptsPlanned(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	opID, _ := s.BeginOp("trash", "", 0, true, opPlans())
	if err := s.FinishItem(opID, "/op/a.bin", "", "", StateDone, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil { // 模拟进程退出（未 Finalize）
		t.Fatal(err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	_, items, err := s2.GetOp(opID)
	if err != nil {
		t.Fatal(err)
	}
	if got := itemState(t, items, "/op/a.bin"); got.State != StateDone {
		t.Fatalf("已收口条目被误伤: %+v", got)
	}
	for _, p := range []string{"/op/b.bin", "/op/c.bin"} {
		if got := itemState(t, items, p); got.State != StateInterrupted {
			t.Fatalf("%s state = %s, 期望 interrupted", p, got.State)
		}
	}
}

func TestListOpsAndClear(t *testing.T) {
	s := testDB(t)
	for i := 0; i < 3; i++ {
		opID, err := s.BeginOp("hardlink", "", 0, true, opPlans()[:1])
		if err != nil {
			t.Fatal(err)
		}
		if err := s.FinalizeOp(opID); err != nil {
			t.Fatal(err)
		}
	}
	ops, err := s.ListOps()
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 3 {
		t.Fatalf("ListOps = %d", len(ops))
	}
	for i := 1; i < len(ops); i++ {
		if ops[i-1].ID <= ops[i].ID {
			t.Fatalf("未按新→旧排序: %+v", ops)
		}
	}
	if err := s.ClearOps(); err != nil {
		t.Fatal(err)
	}
	ops, _ = s.ListOps()
	if len(ops) != 0 {
		t.Fatalf("ClearOps 后仍有 %d 条", len(ops))
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM op_items`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("op_items 未级联清空: %d", n)
	}
}
