package main

// APP-4（2026-09-21 全量审查，设计稿 §15.0-A）：批量回撤收不到"上次失败"的项。
//
// UndoOperation 的函数文档写着："回撤失败的项保持 undo_failed，用户可修正后
// 再次回撤"，单项通道 UndoOperationItem 也确实放行 `done || undo_failed`
// （app.go 状态校验那一行）。但批量入口的 todo 只收 StateDone ——
// 于是"部分失败后用户修好问题、再点一次全部回撤"这条被承诺的路径走不通：
// todo 为空，一个条目都不动，前端按 res.OK==0 && failed 为空弹出
// "该记录没有可回撤的条目（已回撤或未实际执行）"（scan.ts 回撤收尾），
// 而事实是"有一项上次失败了，现在可以撤了"。
//
// ★ 关于设计稿 §15.0-A 里那句"结果文案带『另有 N 项此前失败』"：本批**未实施**。
// 它要在界面上新增一个此前不存在的计数（重试行数），撞裁定③（新增计数的界面
// 呈现归 M8）。假话本身由"todo 收 undo_failed"这一处修掉——修完后那句
// "没有可回撤的条目"只在真的没有时才出现，不再需要新数字来纠偏。

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"filededup/internal/history"
	"filededup/internal/ops"
)

// failedThenFixedOp 落一条两项的回收站账本，并把其中一项标成上次回撤失败。
// 返回 (opID, 上次失败那项的原路径, 其回收站落点)。
func failedThenFixedOp(t *testing.T, a *App) (int64, string, string) {
	t.Helper()
	dir := t.TempDir()
	content := []byte("APP-4-RETRY-AFTER-UNDO-FAILED")
	mtime := time.Now().Add(-24 * time.Hour).UnixNano()

	origs := make([]string, 2)
	dests := make([]string, 2)
	hashes := make([][32]byte, 2)
	for i := range origs {
		dests[i] = filepath.Join(dir, "trash", string(rune('a'+i))+".bin")
		origs[i] = filepath.Join(dir, "home", string(rune('a'+i))+".bin")
		if err := os.MkdirAll(filepath.Dir(dests[i]), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dests[i], content, 0o644); err != nil {
			t.Fatal(err)
		}
		hashes[i] = blake3Of(t, dests[i])
	}
	opID := seedExecutedOp(t, a, "trash", origs, dests, uint64(len(content)), hashes, mtime)
	_, items, err := a.hist.GetOp(opID)
	if err != nil || len(items) != 2 {
		t.Fatalf("账本条目未备好: n=%d err=%v", len(items), err)
	}
	// 只把第二项标成"上次回撤失败"（第一项维持 done 作对照）。
	if err := a.hist.MarkItemUndo(items[1].ID, history.StateUndoFailed, "模拟：上次回撤时目标位置被占用"); err != nil {
		t.Fatal(err)
	}
	return opID, origs[1], dests[1]
}

// TestUndoOperationRetriesPreviouslyFailedItems 批量回撤必须把 undo_failed 项再试一次。
func TestUndoOperationRetriesPreviouslyFailedItems(t *testing.T) {
	a, rec := newHistApp(t)
	opID, orig, _ := failedThenFixedOp(t, a)

	var attempted []string
	prev := undoOneFn
	t.Cleanup(func() { undoOneFn = prev })
	undoOneFn = func(it ops.UndoItem) (string, error) {
		attempted = append(attempted, it.OrigPath)
		return prev(it)
	}

	if _, err := a.UndoOperation(opID); err != nil {
		t.Fatal(err)
	}
	waitUndoDone(t, rec)

	var sawRetry bool
	for _, p := range attempted {
		if p == orig {
			sawRetry = true
		}
	}
	if !sawRetry {
		t.Fatalf("批量回撤没有重试上次失败的项 %s（本次只尝试了 %v）："+
			"与函数文档承诺的「用户可修正后再次回撤」相反（§15.0-A APP-4）", orig, attempted)
	}
	if data, err := os.ReadFile(orig); err != nil || string(data) != "APP-4-RETRY-AFTER-UNDO-FAILED" {
		t.Fatalf("重试的项没回到原位: err=%v", err)
	}
	_, items, err := a.hist.GetOp(opID)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.OrigPath != orig {
			continue
		}
		if it.State != history.StateUndone {
			t.Fatalf("重试成功后该项状态 = %q, want undone（失败原因 %q 也该一并清掉）", it.State, it.Err)
		}
		if it.Err != "" {
			t.Fatalf("撤成之后仍留着上次的失败原因: %q", it.Err)
		}
	}
}

// TestUndoOperationStillSkipsUndoneItems 负控制：已撤成（undone）的项不得再动。
//
// 没有这条，"把 undo_failed 加进 todo"可以靠"干脆不看状态、全表重试"蒙过去，
// 而那样会把已回家、甚至已被用户删掉的文件再撤一次。
func TestUndoOperationStillSkipsUndoneItems(t *testing.T) {
	a, rec := newHistApp(t)
	dir := t.TempDir()
	content := []byte("APP-4-NEGATIVE-CONTROL")
	orig := filepath.Join(dir, "home", "a.bin")
	dest := filepath.Join(dir, "trash", "a.bin")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, content, 0o644); err != nil {
		t.Fatal(err)
	}
	opID := seedExecutedOp(t, a, "trash", []string{orig}, []string{dest},
		uint64(len(content)), [][32]byte{blake3Of(t, dest)}, time.Now().UnixNano())
	_, items, err := a.hist.GetOp(opID)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.hist.MarkItemUndo(items[0].ID, history.StateUndone, ""); err != nil {
		t.Fatal(err)
	}

	called := 0
	prev := undoOneFn
	t.Cleanup(func() { undoOneFn = prev })
	undoOneFn = func(it ops.UndoItem) (string, error) {
		called++
		return prev(it)
	}
	if _, err := a.UndoOperation(opID); err != nil {
		t.Fatal(err)
	}
	waitUndoDone(t, rec)
	if called != 0 {
		t.Fatalf("undone 的项被重试了 %d 次：状态过滤不能放宽到 undone", called)
	}
}
