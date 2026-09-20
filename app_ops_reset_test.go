package main

// M8（2026-09-21 全仓审计 §五 8）：终止事件必须发在在途标志复位**之后**。
//
// goTask 的 `defer reset()` 在 body() 之后执行，而 ops:done / ops:undo:done 是
// body 的最后一句——前端收到 done 的那一刻 opsRunning 仍是 true，于是 done 回调里
// 立刻重扫或重置保留策略都会被「清理操作执行中」误拒。同一缺陷在扫描路径已修
// （`resetInFlight`，见 StartScan 收尾的注释），ops/undo 三条路径当时没覆盖。
//
// 探针的做法是**照前端那样在回调里立刻动手**：把 a.emit 包一层，在收到终止事件的
// 那一刻同步调用 StartScan / ClearKeepDecisions。断言的不是内部字段，而是
// "用户下一步会不会被误拒"。

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"filededup/internal/history"
	"filededup/internal/model"
)

// probeOnEvent 包一层 emit：事件名为 name 时，在**转发给下游之前**执行 probe。
// 这正是"前端在同一拍里立刻动手"的时序。
func probeOnEvent(a *App, name string, probe func()) {
	next := a.emit
	a.emit = func(ctx context.Context, event string, args ...interface{}) {
		if event == name {
			probe()
		}
		next(ctx, event, args...)
	}
}

// probeReport 探针在终止事件那一刻观察到的事实。
type probeReport struct {
	runningStill bool
	scanErr      error
	keepErr      error
}

// TestOpsDoneEmittedAfterRunningReset 清理完成那一刻必须已可再次扫描/重置策略。
func TestOpsDoneEmittedAfterRunningReset(t *testing.T) {
	a, rec, _ := linkedHistApp(t)
	root := t.TempDir()

	reports := make(chan probeReport, 1)
	probeOnEvent(a, "ops:done", func() {
		a.mu.Lock()
		running := a.opsRunning
		a.mu.Unlock()
		_, serr := a.StartScan(model.ScanConfig{Roots: []string{root}, Threads: 1})
		reports <- probeReport{runningStill: running, scanErr: serr, keepErr: a.ClearKeepDecisions()}
	})

	a.mu.Lock()
	var sel []uint64
	for _, f := range a.groups[0].Files {
		sel = append(sel, f.ID)
	}
	a.mu.Unlock()
	if _, err := a.ExecuteOperation(model.OpRequest{Kind: "trash", FileIDs: sel}); err != nil {
		t.Fatal(err)
	}

	// ① 先收探针报告：它由探针自己填，不依赖事件顺序，失败也立刻可见（不必等超时）。
	var rep probeReport
	select {
	case rep = <-reports:
	case <-time.After(5 * time.Second):
		t.Fatal("探针没有被调用：ops:done 没走 a.emit？")
	}
	if rep.runningStill {
		t.Fatal("ops:done 送达那一刻 opsRunning 仍为 true")
	}
	if rep.scanErr != nil {
		t.Fatalf("done 回调里立刻重扫被误拒: %v", rep.scanErr)
	}
	if rep.keepErr != nil {
		t.Fatalf("done 回调里立刻重置保留策略被误拒: %v", rep.keepErr)
	}
	// ② 内嵌的那次重扫必须自己收尾，不能留 goroutine 进 teardown。
	// 按集合收而不按顺序等：探针在转发 ops:done **之前**就发起了重扫，
	// 空目录扫描可能先发出 scan:done。
	drainTerminals(t, rec, 10*time.Second, "ops:done", "scan:done")
}

// TestUndoDoneEmittedAfterRunningReset 回撤同口径：undo:done 之后立刻再操作不得被误拒。
func TestUndoDoneEmittedAfterRunningReset(t *testing.T) {
	dir := t.TempDir()
	content := []byte("M8-UNDO-RESET-ORDER!!!")
	dest := filepath.Join(dir, "trash", "a.bin")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, content, 0o644); err != nil {
		t.Fatal(err)
	}
	orig := filepath.Join(dir, "home", "a.bin")
	a, rec := newHistApp(t)
	opID := seedExecutedOp(t, a, "trash", []string{orig}, []string{dest},
		uint64(len(content)), [][32]byte{blake3Of(t, dest)}, time.Now().UnixNano())

	var refused []string
	probeOnEvent(a, "ops:undo:done", func() {
		a.mu.Lock()
		running := a.opsRunning
		a.mu.Unlock()
		if running {
			refused = append(refused, "opsRunning 仍为 true")
		}
		if err := a.ClearKeepDecisions(); err != nil {
			refused = append(refused, "ClearKeepDecisions: "+err.Error())
		}
	})

	if _, err := a.UndoOperation(opID); err != nil {
		t.Fatal(err)
	}
	waitUndoDone(t, rec)
	if len(refused) > 0 {
		t.Fatalf("undo:done 那一刻用户下一步被误拒: %v", refused)
	}
	// 反向守卫：复位提前不能以"少做事"为代价——文件必须真的回了原位、账本必须收口。
	if _, err := os.Stat(orig); err != nil {
		t.Fatalf("回撤未落地: %v", err)
	}
	_, items, err := a.hist.GetOp(opID)
	if err != nil {
		t.Fatal(err)
	}
	if items[0].State != history.StateUndone {
		t.Fatalf("收口状态 = %s, want undone", items[0].State)
	}
}

// TestUndoItemDoneEmittedAfterRunningReset 单项回撤（UndoOperationItem）是同一段
// 收尾代码的第三份拷贝，也必须同口径。
// 三条路径分别变异验证：只回退其中一条，对应这一条用例会红。
func TestUndoItemDoneEmittedAfterRunningReset(t *testing.T) {
	dir := t.TempDir()
	content := []byte("M8-UNDO-ITEM-ORDER!!!")
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
		uint64(len(content)), [][32]byte{blake3Of(t, dest)}, time.Now().UnixNano())
	_, items, err := a.hist.GetOp(opID)
	if err != nil || len(items) != 1 {
		t.Fatalf("账本条目未备好: n=%d err=%v", len(items), err)
	}

	var refused []string
	probeOnEvent(a, "ops:undo:done", func() {
		a.mu.Lock()
		running := a.opsRunning
		a.mu.Unlock()
		if running {
			refused = append(refused, "opsRunning 仍为 true")
		}
	})

	if _, err := a.UndoOperationItem(opID, items[0].ID); err != nil {
		t.Fatal(err)
	}
	waitUndoDone(t, rec)
	if len(refused) > 0 {
		t.Fatalf("单项回撤的 undo:done 那一刻仍被误拒: %v", refused)
	}
	if _, err := os.Stat(orig); err != nil {
		t.Fatalf("回撤未落地: %v", err)
	}
}

// drainTerminals 从终止队列收齐 want 全部事件（不保证顺序）；收不齐即失败。
// 不"按顺序等"是因为探针在转发终止事件之前就可能又发了下一条终止事件。
func drainTerminals(t *testing.T, rec *eventRecorder, max time.Duration, want ...string) {
	t.Helper()
	seen := map[string]bool{}
	deadline := time.After(max)
	for len(seen) < len(want) {
		select {
		case ev := <-rec.done:
			for _, w := range want {
				if ev == w {
					seen[ev] = true
				}
			}
		case <-deadline:
			t.Fatalf("等待终止事件 %v 超时，只见到 %v", want, seen)
		}
	}
}
