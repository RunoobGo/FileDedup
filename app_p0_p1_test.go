package main

// P0-1 / P1-1 回归：扫描-操作生命周期与互斥。
// 这些路径跑在 goroutine 里并以事件收尾，此前因无法注入事件出口而完全无测试覆盖。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"filededup/internal/model"
)

// eventRecorder 替换 a.emit，记录终止事件。
type eventRecorder struct {
	mu     sync.Mutex
	events []string
	done   chan string
}

func (e *eventRecorder) emit(_ context.Context, name string, _ ...interface{}) {
	e.mu.Lock()
	e.events = append(e.events, name)
	e.mu.Unlock()
	switch name {
	case "scan:done", "scan:error", "scan:cancelled", "ops:done", "ops:error":
		select {
		case e.done <- name:
		default:
		}
	}
}

func (e *eventRecorder) waitTerminal(t *testing.T, tag string) string {
	t.Helper()
	select {
	case ev := <-e.done:
		return ev
	case <-time.After(30 * time.Second):
		t.Fatalf("%s: 未收到扫描终止事件（疑似永久卡住）", tag)
		return ""
	}
}

func newProbeApp(t *testing.T) (*App, *eventRecorder, string) {
	t.Helper()
	root := t.TempDir()
	payload := []byte("SHARED-DUPLICATE-CONTENT-PAYLOAD")
	os.WriteFile(filepath.Join(root, "a.bin"), payload, 0o644)
	os.MkdirAll(filepath.Join(root, "sub"), 0o755)
	os.WriteFile(filepath.Join(root, "sub", "b.bin"), payload, 0o644)

	a := NewApp()
	a.ctx = context.Background() // emit 已替换，不再要求 Wails 内部 context
	a.cfgDir = t.TempDir()
	rec := &eventRecorder{done: make(chan string, 8)}
	a.emit = rec.emit
	return a, rec, root
}

// P0-1：连续两次扫描都必须正常完成（修正前第二次被状态机拒绝，
// 而 StartScan 已清空结果集 → 前端永久卡在「扫描中」且结果为空）。
func TestSecondScanCompletes(t *testing.T) {
	a, rec, root := newProbeApp(t)

	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}}); err != nil {
		t.Fatalf("StartScan#1: %v", err)
	}
	if ev := rec.waitTerminal(t, "scan#1"); ev != "scan:done" {
		t.Fatalf("scan#1 终止事件 = %s", ev)
	}
	r1, _ := a.GetResultGroups(ResultQuery{})
	if r1.Total == 0 {
		t.Fatal("scan#1 应至少找出 1 组")
	}

	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}}); err != nil {
		t.Fatalf("P0-1: StartScan#2 被拒: %v", err)
	}
	if ev := rec.waitTerminal(t, "scan#2"); ev != "scan:done" {
		t.Fatalf("P0-1: scan#2 终止事件 = %s（期望 scan:done）", ev)
	}
	r2, _ := a.GetResultGroups(ResultQuery{})
	if r2.Total != r1.Total {
		t.Fatalf("P0-1: 二次扫描结果不一致: %d vs %d", r2.Total, r1.Total)
	}
}

// P0-1：取消后也能重新扫描。
func TestScanAfterCancel(t *testing.T) {
	a, rec, root := newProbeApp(t)
	// 造足够大的数据集，保证 Cancel 落在运行中而非收尾后
	for i := 0; i < 4000; i++ {
		os.WriteFile(filepath.Join(root, fmt.Sprintf("x%04d.bin", i)),
			[]byte(fmt.Sprintf("payload-%d", i%7)), 0o644)
	}
	id1, err := a.StartScan(model.ScanConfig{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	for a.GetStatus() == string(model.StatusIdle) {
		time.Sleep(2 * time.Millisecond)
	}
	if err := a.CancelScan(); err != nil {
		t.Fatalf("运行中取消应成功: %v", err)
	}
	ev := rec.waitTerminal(t, "scan#1(cancel)")
	if ev != "scan:cancelled" {
		t.Skipf("扫描在取消前已完成（环境过快），跳过：taskID=%s ev=%s", id1, ev)
	}
	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}}); err != nil {
		t.Fatalf("P0-1: 取消后重新扫描被拒: %v", err)
	}
	if ev := rec.waitTerminal(t, "scan#2"); ev != "scan:done" && ev != "scan:cancelled" {
		t.Fatalf("scan#2 终止事件 = %s", ev)
	}
	// P3：终态下再取消应报错而不是静默成功
	if err := a.CancelScan(); err == nil {
		t.Error("P3: 任务已结束时 CancelScan 应返回错误")
	}
}

// P1-1：清理操作在途时不得开启新扫描（否则 ops goroutine 收尾会覆盖新结果集）。
func TestStartScanRejectedWhileOpsRunning(t *testing.T) {
	a, _, root := newProbeApp(t)
	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}}); err != nil {
		t.Fatal(err)
	}
	// 等扫描完成
	time.Sleep(50 * time.Millisecond)
	for a.GetStatus() != string(model.StatusDone) {
		time.Sleep(20 * time.Millisecond)
	}
	a.opsRunning = true // 模拟 ops goroutine 在途
	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}}); err == nil {
		t.Fatal("P1-1: 操作执行中应拒绝新扫描（结果集会被陈旧回写覆盖）")
	}
	a.opsRunning = false
	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}}); err != nil {
		t.Fatalf("空闲时应可扫描: %v", err)
	}
}

// P1-1 对称：扫描在途时不得开启清理操作。
func TestExecuteOperationRejectedWhileScanInFlight(t *testing.T) {
	a, _, root := newProbeApp(t)
	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}}); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.groups = []*model.DuplicateGroup{mkGroup(1, 100, "a", "b")}
	a.byID[101] = a.groups[0].Files[0]
	a.scanInFlight = true // 模拟扫描 goroutine 收尾在途
	a.mu.Unlock()
	if _, err := a.ExecuteOperation(model.OpRequest{Kind: "trash", FileIDs: []uint64{101}}); err == nil {
		t.Fatal("P1-1: 扫描在途时应拒绝清理操作")
	}
}

// P1-1：并发「扫描 + 清理」不得同时被受理（互斥必须双向闭合）。
func TestScanAndOpsAreMutuallyExclusive(t *testing.T) {
	a, _, root := newProbeApp(t)
	var scanOK, opsOK int
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}}); err == nil {
					scanOK++
				}
				return
			}
			a.mu.Lock()
			a.groups = []*model.DuplicateGroup{mkGroup(1, 100, "a", "b")}
			a.byID[101] = a.groups[0].Files[0]
			a.mu.Unlock()
			if _, err := a.ExecuteOperation(model.OpRequest{Kind: "trash", FileIDs: []uint64{101}}); err == nil {
				opsOK++
			}
		}(i)
	}
	wg.Wait()
	// 允许其中一类成功，但两者同时成功说明互斥有洞
	if scanOK > 0 && opsOK > 0 {
		t.Fatalf("P1-1: 扫描与清理同时被受理 scanOK=%d opsOK=%d", scanOK, opsOK)
	}
}
