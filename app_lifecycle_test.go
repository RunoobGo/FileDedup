package main

// 2026-09-18 审查 C5 回归：关窗拦截在途任务 + 退出时先收口再关句柄。
//
// 关心两件事：
//  1. 有任务在途时第一次按关闭必须被拦下（并请求中止），第二次才走人；
//  2. shutdown 不得抢在 goroutine 收尾之前释放 cache/hist 句柄，
//     否则扫描历史与清理账本会静默少一条。

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"filededup/internal/history"
	"filededup/internal/model"
)

// lifecycleSpy 记录事件与 forceExit 调用（emit 桩需要看到 payload）。
type lifecycleSpy struct {
	mu     sync.Mutex
	events []struct {
		name string
		data map[string]string
	}
	exits []int
}

func (s *lifecycleSpy) emit(_ context.Context, name string, data ...interface{}) {
	m := map[string]string{}
	if len(data) > 0 {
		if v, ok := data[0].(map[string]string); ok {
			m = v
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, struct {
		name string
		data map[string]string
	}{name, m})
}

func (s *lifecycleSpy) recordExit(code int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exits = append(s.exits, code)
}

func (s *lifecycleSpy) count(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, e := range s.events {
		if e.name == name {
			n++
		}
	}
	return n
}

func (s *lifecycleSpy) lastData(name string) map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.events) - 1; i >= 0; i-- {
		if s.events[i].name == name {
			return s.events[i].data
		}
	}
	return nil
}

func (s *lifecycleSpy) exitCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.exits)
}

func newLifecycleApp(t *testing.T) (*App, *lifecycleSpy) {
	t.Helper()
	a := NewApp()
	a.ctx = context.Background()
	spy := &lifecycleSpy{}
	a.emit = spy.emit
	a.forceExit = spy.recordExit
	return a, spy
}

// setBusy 在锁内伪造在途状态（并挂一个可观测的取消函数）。
func setBusy(t *testing.T, a *App, ops, scan bool) context.Context {
	opCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a.mu.Lock()
	a.opsRunning = ops
	a.scanInFlight = scan
	if ops {
		a.opsCancel = cancel
	}
	a.mu.Unlock()
	return opCtx
}

func TestBeforeCloseAllowsQuitWhenIdle(t *testing.T) {
	a, spy := newLifecycleApp(t)
	if a.beforeClose(context.Background()) {
		t.Fatal("空闲时不得拦下关闭")
	}
	if spy.count("app:quit-blocked") != 0 {
		t.Fatal("空闲关闭不应发提示事件")
	}
}

// 清理在途：第一次关闭 → 拦下 + 请求中止 + 提示。
func TestBeforeCloseBlocksOpsAndRequestsCancel(t *testing.T) {
	a, spy := newLifecycleApp(t)
	opCtx := setBusy(t, a, true, false)

	if !a.beforeClose(context.Background()) {
		t.Fatal("清理在途时必须拦下关闭")
	}
	select {
	case <-opCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("拦下关闭时必须同时请求中止在途清理")
	}
	if spy.count("app:quit-blocked") != 1 {
		t.Fatalf("提示事件 = %d, want 1", spy.count("app:quit-blocked"))
	}
	data := spy.lastData("app:quit-blocked")
	if data["running"] != "ops" {
		t.Errorf("running = %q, want ops", data["running"])
	}
	if data["message"] == "" {
		t.Error("提示缺 message，前端 toast 会显示空条")
	}
	if spy.exitCalls() != 0 {
		t.Fatal("第一次关闭不得强退")
	}
}

// 扫描在途同样拦（且提示要说"扫描"，别谎报清理）。
func TestBeforeCloseBlocksScan(t *testing.T) {
	a, spy := newLifecycleApp(t)
	setBusy(t, a, false, true)
	if !a.beforeClose(context.Background()) {
		t.Fatal("扫描在途时必须拦下关闭")
	}
	if got := spy.lastData("app:quit-blocked")["running"]; got != "scan" {
		t.Errorf("running = %q, want scan", got)
	}
}

// 第二次关闭：认定用户就是要走，立即退出（不等收尾），不再发提示。
func TestBeforeCloseSecondAttemptExits(t *testing.T) {
	a, spy := newLifecycleApp(t)
	setBusy(t, a, true, false)

	a.beforeClose(context.Background())
	if !a.beforeClose(context.Background()) {
		t.Fatal("拦下后应返回 prevent，由 forceExit 决定去留")
	}
	if spy.exitCalls() != 1 {
		t.Fatalf("forceExit 调用 = %d, want 1", spy.exitCalls())
	}
	if spy.count("app:quit-blocked") != 1 {
		t.Errorf("强退时不该再发一次提示: %d", spy.count("app:quit-blocked"))
	}
}

// 任务停下后再关窗 = 正常退出；且拦下计数归零，不会"攒够两次就强退"。
func TestBeforeClosePendingResetsAfterIdle(t *testing.T) {
	a, spy := newLifecycleApp(t)
	setBusy(t, a, true, false)
	a.beforeClose(context.Background())

	a.mu.Lock()
	a.opsRunning = false
	a.opsCancel = nil
	a.mu.Unlock()
	if a.beforeClose(context.Background()) {
		t.Fatal("空闲后应放行关闭")
	}

	setBusy(t, a, true, false)
	a.beforeClose(context.Background())
	if spy.exitCalls() != 0 {
		t.Fatal("新一轮的第一次关闭必须只是拦下")
	}
	if spy.count("app:quit-blocked") != 2 {
		t.Errorf("提示事件 = %d, want 2", spy.count("app:quit-blocked"))
	}
}

// shutdown 必须等在途 goroutine 收尾（含写历史）之后再关句柄。
func TestShutdownWaitsForInflightTail(t *testing.T) {
	a, _ := newLifecycleApp(t)
	a.cfgDir = t.TempDir()
	hs, err := history.Open(filepath.Join(a.cfgDir, "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	a.hist = hs

	started := make(chan struct{})
	release := make(chan struct{})
	var tailErr error
	var wrote atomic.Bool
	a.goTask("lifecycle-test", func() {}, func() {
		close(started)
		<-release
		g := mkHistGroup(1, 1000, "/u/a/x.bin", "/u/b/y.bin")
		_, tailErr = a.hist.SaveScan(model.ScanConfig{Roots: []string{"/u"}},
			[]*model.DuplicateGroup{g}, nil)
		wrote.Store(true)
	})
	<-started

	shutdownDone := make(chan struct{})
	go func() {
		a.shutdown(context.Background())
		close(shutdownDone)
	}()
	time.Sleep(30 * time.Millisecond) // 让 shutdown 走到等待处
	close(release)
	<-shutdownDone

	if !wrote.Load() {
		t.Fatal("shutdown 未等待在途任务收尾就返回了")
	}
	if tailErr != nil {
		t.Fatalf("收尾落账失败（句柄被提前关闭）: %v", tailErr)
	}
	// shutdown 已把句柄关掉了：重新打开才能验证「落账真的持久化」而非
	// 只是写了内存（同时证明收尾是在干净关闭之前完成的）。
	hs2, err := history.Open(filepath.Join(a.cfgDir, "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer hs2.Close()
	scans, err := hs2.ListScans()
	if err != nil {
		t.Fatal(err)
	}
	if len(scans) != 1 {
		t.Fatalf("历史行数 = %d, want 1", len(scans))
	}
}

func TestWaitGroupTimeout(t *testing.T) {
	var wg sync.WaitGroup
	if !waitGroupTimeout(&wg, time.Second) {
		t.Fatal("空 WaitGroup 应立即判定完成")
	}
	wg.Add(1)
	if waitGroupTimeout(&wg, 30*time.Millisecond) {
		t.Fatal("仍有在途任务时不得判定完成")
	}
	done := make(chan bool, 1)
	go func() { done <- waitGroupTimeout(&wg, 2*time.Second) }()
	time.Sleep(30 * time.Millisecond)
	wg.Done()
	if !<-done {
		t.Fatal("任务归零后应及时判定完成")
	}
}
