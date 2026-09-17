package dedup

// P3 回归：Pause/Resume/Cancel 必须把"无任务时空转"告知调用方，
// 以及 panic 兜底用的 Abort 必须把状态机收敛回终态。
//
// 修正前三个方法无返回值（恒等于成功），前端据此把按钮切成"已暂停"，
// 用户以为生效、实际根本没暂停；Done 之后点"取消"同样毫无反馈。

import (
	"context"
	"testing"

	"filededup/internal/model"
)

func TestControlErrorsWhenNoRunningTask(t *testing.T) {
	p := New() // Idle
	if err := p.Pause(); err == nil {
		t.Error("Idle 下 Pause 应报错")
	}
	if err := p.Resume(); err == nil {
		t.Error("Idle 下 Resume 应报错")
	}
	if err := p.Cancel(); err == nil {
		t.Error("Idle 下 Cancel 应报错（无在途任务）")
	}
	if p.Status() != model.StatusIdle {
		t.Errorf("空转调用不得改动状态: %s", p.Status())
	}

	// 终态同样应报错，而不是静默成功
	for _, s := range []model.TaskStatus{model.StatusDone, model.StatusCancelled, model.StatusFailed} {
		p.mu.Lock()
		p.status = s
		p.afterResume = ""
		p.cancel = nil
		p.mu.Unlock()
		if err := p.Pause(); err == nil {
			t.Errorf("%s 下 Pause 应报错", s)
		}
		if err := p.Resume(); err == nil {
			t.Errorf("%s 下 Resume 应报错", s)
		}
		if err := p.Cancel(); err == nil {
			t.Errorf("%s 下 Cancel 应报错（无可取消的在途任务）", s)
		}
		if got := p.Status(); got != s {
			t.Errorf("%s 下空转调用改动了状态: %s", s, got)
		}
	}
}

// 白盒注入运行态：无需真实跑完一轮即可断言"该成功时确实成功"。
func TestControlSucceedWhileRunning(t *testing.T) {
	var cancelled int
	for _, run := range []model.TaskStatus{model.StatusScanning, model.StatusPrefiltering, model.StatusHashing} {
		p := New()
		p.mu.Lock()
		p.status = run
		p.cancel = func() { cancelled++ }
		p.mu.Unlock()

		if err := p.Pause(); err != nil {
			t.Fatalf("%s 下 Pause 应成功: %v", run, err)
		}
		if p.Status() != model.StatusPaused {
			t.Fatalf("%s Pause 后状态 = %s", run, p.Status())
		}
		if err := p.Pause(); err == nil {
			t.Errorf("重复 Pause 应报错（已处于暂停态）")
		}
		// 暂停中 Resume 应回到原运行阶段，而不是无脑回到 Scanning
		if err := p.Resume(); err != nil {
			t.Fatalf("Resume 应成功: %v", err)
		}
		if got := p.Status(); got != run {
			t.Errorf("%s 暂停后恢复应回到原阶段，got %s", run, got)
		}
		if err := p.Resume(); err == nil {
			t.Errorf("未暂停时 Resume 应报错")
		}
		if err := p.Cancel(); err != nil {
			t.Errorf("运行中 Cancel 应成功: %v", err)
		}
	}
	if cancelled != 3 {
		t.Errorf("Cancel 未真正触发取消函数: %d", cancelled)
	}
}

func TestAbortConvergesRunningState(t *testing.T) {
	for _, s := range []model.TaskStatus{model.StatusScanning, model.StatusPrefiltering, model.StatusHashing, model.StatusPaused} {
		p := New()
		p.mu.Lock()
		p.status = s
		p.afterResume = model.StatusHashing
		p.mu.Unlock()
		p.Abort()
		if got := p.Status(); got != model.StatusFailed {
			t.Fatalf("%s 下 Abort 应收敛为 Failed，got %s", s, got)
		}
		if p.afterResume != "" {
			t.Errorf("Abort 应清空待恢复阶段，got %s", p.afterResume)
		}
		// 收敛后必须能重新 Run（P0-1 的前提）
		if _, _, err := p.Run(context.Background(), model.ScanConfig{Roots: []string{t.TempDir()}}); err != nil {
			t.Errorf("%s Abort 后应可重新扫描: %v", s, err)
		}
	}
}

func TestAbortKeepsTerminalAndIdle(t *testing.T) {
	for _, s := range []model.TaskStatus{model.StatusIdle, model.StatusDone, model.StatusCancelled, model.StatusFailed} {
		p := New()
		p.mu.Lock()
		p.status = s
		p.mu.Unlock()
		p.Abort()
		if got := p.Status(); got != s {
			t.Errorf("%s 不应被 Abort 改写: got %s", s, got)
		}
	}
}
