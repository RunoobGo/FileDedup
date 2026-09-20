package main

// 2026-09-18 审查 I6：回撤写前落账（账本先落、文件后动）与崩溃残留自愈。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filededup/internal/hasher"
	"filededup/internal/history"
	"filededup/internal/ops"
)

// blake3Of 全量内容哈希（账本里的内容证据）。
func blake3Of(t *testing.T, p string) [32]byte {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	pool := hasher.NewPool()
	buf := pool.GetStreamBuf()
	defer pool.PutStreamBuf(buf)
	h, err := hasher.HashFull(f, st.Size(), buf)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// seedExecutedOp 落一条真实可撤的账：done + 落点 + 内容/时间戳证据。
func seedExecutedOp(t *testing.T, a *App, kind string, origs, dests []string,
	size uint64, hashes [][32]byte, mtimeNs int64) int64 {
	t.Helper()
	plans := make([]history.OpItemPlan, len(origs))
	for i, p := range origs {
		plans[i] = history.OpItemPlan{OrigPath: p, Size: size, Hash: hashes[i], MtimeNs: mtimeNs}
	}
	opID, err := a.hist.BeginOp(kind, "", 0, true, plans)
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range origs {
		if err := a.hist.FinishItem(opID, p, dests[i], "", history.StateDone, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.hist.FinalizeOp(opID); err != nil {
		t.Fatal(err)
	}
	return opID
}

// 动作发生的「那一刻」账本必须已写下 undoing——顺序反过来（先动文件后落账）
// 就是本次审查的缺陷：中途被杀后账本永久停在 done。
func TestUndoLedgerMarkedBeforeFileSystemAction(t *testing.T) {
	dir := t.TempDir()
	content := []byte("I6-ORDER-OF-MARK-AND-MOVE")
	mtime := time.Now().Add(-24 * time.Hour).UnixNano()
	dest := filepath.Join(dir, "trash", "a.bin")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, content, 0o644); err != nil {
		t.Fatal(err)
	}
	orig := filepath.Join(dir, "home", "a.bin")
	a, rec := newHistApp(t)
	hashes := [][32]byte{blake3Of(t, dest)}
	opID := seedExecutedOp(t, a, "trash", []string{orig}, []string{dest},
		uint64(len(content)), hashes, mtime)

	prev := undoOneFn
	t.Cleanup(func() { undoOneFn = prev })
	var stateAtAction string
	undoOneFn = func(it ops.UndoItem) (string, error) {
		_, items, err := a.hist.GetOp(opID)
		if err != nil {
			t.Error(err)
		}
		stateAtAction = items[0].State
		return prev(it)
	}

	if _, err := a.UndoOperation(opID); err != nil {
		t.Fatal(err)
	}
	waitUndoDone(t, rec)
	if stateAtAction != history.StateUndoing {
		t.Fatalf("回撤动作发生时账本状态 = %q, want %q（必须先落账再动文件）",
			stateAtAction, history.StateUndoing)
	}
	if got, err := os.ReadFile(orig); err != nil || string(got) != string(content) {
		t.Fatalf("文件未回原位: err=%v", err)
	}
	_, items, err := a.hist.GetOp(opID)
	if err != nil {
		t.Fatal(err)
	}
	if items[0].State != history.StateUndone {
		t.Fatalf("收口状态 = %s, want undone", items[0].State)
	}
}

// 账本写不进去就不动文件（与 C3「无账本不动文件」同口径）。
func TestUndoRefusedWhenWriteAheadMarkFails(t *testing.T) {
	dir := t.TempDir()
	content := []byte("I6-NO-MARK-NO-MOVE!!!!")
	dest := filepath.Join(dir, "trash", "a.bin")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, content, 0o644); err != nil {
		t.Fatal(err)
	}
	a, _ := newHistApp(t)
	prev := undoOneFn
	t.Cleanup(func() { undoOneFn = prev })
	called := false
	undoOneFn = func(ops.UndoItem) (string, error) { called = true; return prev(ops.UndoItem{}) }

	// 条目 ID 不存在 → 写前落账必然失败
	_, err := a.undoExecuteItem(a.hist, "trash", history.OpItem{
		ID: 987654, OrigPath: filepath.Join(dir, "home", "a.bin"),
		DestPath: dest, Size: uint64(len(content)),
	})
	if err == nil || !strings.Contains(err.Error(), "写前落账失败") {
		t.Fatalf("写前落账失败必须拒绝执行: %v", err)
	}
	if called {
		t.Fatal("落账失败后仍调用了回撤执行")
	}
	if _, statErr := os.Stat(dest); statErr != nil {
		t.Fatalf("文件系统被改动了: %v", statErr)
	}
}

// reopenHist 重开历史库以触发启动收口（模拟应用重启）。
func reopenHist(t *testing.T, a *App) {
	t.Helper()
	hs, err := history.Open(filepath.Join(a.cfgDir, "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	old := a.hist
	a.mu.Lock()
	a.hist = hs
	a.mu.Unlock()
	_ = old.Close()
	t.Cleanup(func() { _ = hs.Close() })
}

// 死在「已标 undoing、动作未生效」之间：重启收口为带原因的 undo_failed，
// 单项回撤仍可正常完成（不得变成永久死账）。
func TestUndoResidueBeforeActionRecoverableAfterRestart(t *testing.T) {
	dir := t.TempDir()
	content := []byte("I6-RESIDUE-BEFORE-ACTION")
	mtime := time.Now().Add(-12 * time.Hour).UnixNano()
	dest := filepath.Join(dir, "trash", "b.bin")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, content, 0o644); err != nil {
		t.Fatal(err)
	}
	orig := filepath.Join(dir, "home", "b.bin")
	a, rec := newHistApp(t)
	opID := seedExecutedOp(t, a, "trash", []string{orig}, []string{dest},
		uint64(len(content)), [][32]byte{blake3Of(t, dest)}, mtime)
	_, items, err := a.hist.GetOp(opID)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.hist.MarkItemUndo(items[0].ID, history.StateUndoing, ""); err != nil {
		t.Fatal(err)
	}

	reopenHist(t, a)
	_, items, err = a.hist.GetOp(opID)
	if err != nil {
		t.Fatal(err)
	}
	if items[0].State != history.StateUndoFailed || items[0].Err == "" {
		t.Fatalf("undoing 残留应收口为带原因的 undo_failed: %+v", items[0])
	}

	if _, err := a.UndoOperationItem(opID, items[0].ID); err != nil {
		t.Fatalf("收口后的残留项应可重试: %v", err)
	}
	waitUndoDone(t, rec)
	if got, err := os.ReadFile(orig); err != nil || string(got) != string(content) {
		t.Fatalf("重试未恢复文件: err=%v", err)
	}
	_, items, _ = a.hist.GetOp(opID)
	if items[0].State != history.StateUndone {
		t.Fatalf("重试后状态 = %s, want undone", items[0].State)
	}
}

// 死在「动作已生效、账本未收口」之间：重启后重试按现状判为已回撤，
// 不再报「回收站中的文件已不存在」（本次缺陷的用户可见症状）。
func TestUndoResidueAfterRestoreSelfHeals(t *testing.T) {
	dir := t.TempDir()
	content := []byte("I6-RESIDUE-AFTER-RESTORE")
	mtime := time.Now().Add(-36 * time.Hour).UnixNano()
	dest := filepath.Join(dir, "trash", "c.bin")
	orig := filepath.Join(dir, "home", "c.bin")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, content, 0o644); err != nil {
		t.Fatal(err)
	}
	a, rec := newHistApp(t)
	hash := blake3Of(t, dest)
	opID := seedExecutedOp(t, a, "trash", []string{orig}, []string{dest},
		uint64(len(content)), [][32]byte{hash}, mtime)
	_, items, err := a.hist.GetOp(opID)
	if err != nil {
		t.Fatal(err)
	}
	itemID := items[0].ID
	if err := a.hist.MarkItemUndo(itemID, history.StateUndoing, ""); err != nil {
		t.Fatal(err)
	}
	// 文件系统侧：撤销已生效（搬回原位 + 还原 mtime），账本仍停在 undoing
	if err := os.MkdirAll(filepath.Dir(orig), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dest, orig); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(orig, time.Now(), time.Unix(0, mtime)); err != nil {
		t.Fatal(err)
	}

	reopenHist(t, a)
	if _, err := a.UndoOperationItem(opID, itemID); err != nil {
		t.Fatalf("自愈重试被拒: %v", err)
	}
	waitUndoDone(t, rec)
	_, items, _ = a.hist.GetOp(opID)
	if items[0].State != history.StateUndone || items[0].Err != "" {
		t.Fatalf("已还原的残留应记已回撤，实际: %+v", items[0])
	}
	if got, err := os.ReadFile(orig); err != nil || string(got) != string(content) {
		t.Fatal("原位文件被改动")
	}
}
