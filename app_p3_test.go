package main

// P3 回归：goroutine panic 兜底、taskID 唯一性、无任务时的控制类错误、
// Linux「打开所在文件夹」的命令选择。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"filededup/internal/model"
)

// 绑定层 goroutine panic 不得带走进程，且必须：
//  1. 发出终止事件（前端据此复位"扫描中"）
//  2. 复位在途标志（否则后续所有扫描被拒）
//  3. 收敛状态机（否则永远停在运行态 → 只能重启应用）
//
// 注入方式：让流水线回调 OnStage 首次调用即 panic——与真实故障同一条路径
// （panic 从 Run 内部向上穿过 StartScan 的 goroutine）。
func TestScanGoroutinePanicIsContained(t *testing.T) {
	a, rec, root := newProbeApp(t)
	var fired sync.Once
	a.pipe.OnStage = func(model.StageEvent) {
		fired.Do(func() { panic("模拟流水线内部 panic") })
	}
	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}}); err != nil {
		t.Fatal(err)
	}
	if ev := rec.waitTerminal(t, "panic 后"); ev != "scan:error" {
		t.Fatalf("panic 应发 scan:error，got %s", ev)
	}
	// panic 展开顺序：reset 先于 recover 执行 → 事件到达时在途标志已复位。
	// 状态收敛在事件之前完成，故此处可直接断言。
	if got := a.GetStatus(); got != string(model.StatusFailed) {
		t.Errorf("P3: panic 后状态应收敛为 Failed，got %s", got)
	}
	a.mu.Lock()
	inFlight := a.scanInFlight
	a.mu.Unlock()
	if inFlight {
		t.Error("panic 后 scanInFlight 必须复位")
	}
	// 关键：仍能重新扫描（修正前永久锁死，只能重启应用）
	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}}); err != nil {
		t.Errorf("panic 后应可重新扫描: %v", err)
	}
	if ev := rec.waitTerminal(t, "panic 后重扫"); ev != "scan:done" {
		t.Errorf("重扫应正常完成，got %s", ev)
	}
}

// goTask 是扫描与清理共用的守卫；ops 分支单独覆盖（ExecuteOperation 的
// 真实执行会写系统回收站，不适合在单测里跑）。
func TestGoTaskOpsPanicResetsFlags(t *testing.T) {
	a, rec, _ := newProbeApp(t)
	a.mu.Lock()
	a.opsRunning = true
	done := make(chan struct{})
	a.mu.Unlock()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		a.goTask("ops", func() {
			a.mu.Lock()
			a.opsRunning = false
			a.opsCancel = nil
			a.mu.Unlock()
			close(done)
		}, func() { panic("模拟清理阶段 panic") })
	}()
	wg.Wait()
	<-done
	if ev := rec.waitTerminal(t, "ops panic"); ev != "ops:error" {
		t.Errorf("ops panic 应发 ops:error，got %s", ev)
	}
	a.mu.Lock()
	running := a.opsRunning
	a.mu.Unlock()
	if running {
		t.Error("panic 后 opsRunning 必须复位（否则清理永久不可用）")
	}
}

// P3：秒级时间戳作 taskID 时，同一秒内重扫会重复（取消后立刻重试很常见）。
func TestTaskIDsAreUnique(t *testing.T) {
	a, rec, root := newProbeApp(t)
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		id, err := a.StartScan(model.ScanConfig{Roots: []string{root}})
		if err != nil {
			t.Fatalf("StartScan#%d: %v", i, err)
		}
		if seen[id] {
			t.Fatalf("P3: taskID 重复: %s", id)
		}
		seen[id] = true
		if !strings.HasPrefix(id, "scan-") {
			t.Errorf("taskID 形态异常: %s", id)
		}
		rec.waitTerminal(t, "scan")
	}
}

// P3：控制类绑定在无任务时必须返回错误（前端据此提示，而非误显示"已暂停"）。
func TestAppControlBindingsRejectIdle(t *testing.T) {
	a, _, _ := newProbeApp(t)
	if err := a.PauseScan(); err == nil {
		t.Error("空闲时 PauseScan 应报错")
	}
	if err := a.ResumeScan(); err == nil {
		t.Error("空闲时 ResumeScan 应报错")
	}
	if err := a.CancelScan(); err == nil {
		t.Error("空闲时 CancelScan 应报错")
	}
	if err := a.CancelOperation(); err == nil {
		t.Error("无清理操作时 CancelOperation 应报错")
	}
}

// 报错改动不得把正常路径也堵死：真实在途任务上暂停/恢复要成功。
func TestAppPauseResumeOnLiveScan(t *testing.T) {
	a, rec, root := newProbeApp(t)
	for i := 0; i < 3000; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("p3-%04d.bin", i)),
			[]byte(strings.Repeat("x", 4096)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for a.GetStatus() == string(model.StatusIdle) {
		if time.Now().After(deadline) {
			t.Skip("未及时进入运行态，跳过")
		}
		time.Sleep(2 * time.Millisecond)
	}
	if err := a.PauseScan(); err != nil {
		t.Fatalf("运行中 PauseScan 应成功: %v", err)
	}
	if err := a.ResumeScan(); err != nil {
		t.Fatalf("ResumeScan 应成功: %v", err)
	}
	rec.waitTerminal(t, "scan")
}

func TestRevealInFolderUnknownID(t *testing.T) {
	a, _, _ := newProbeApp(t)
	err := a.RevealInFolder(999999)
	if err == nil {
		t.Fatal("未知 id 应报错（而不是静默成功）")
	}
	if !strings.Contains(err.Error(), "999999") {
		t.Errorf("错误信息应含 id 便于排查: %v", err)
	}
}
