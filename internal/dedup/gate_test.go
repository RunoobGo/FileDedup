package dedup

import (
	"context"
	"testing"
	"time"

	"filededup/internal/model"
)

func TestGatePauseResume(t *testing.T) {
	g := NewGate()
	if g.Paused() {
		t.Fatal("初始应运行态")
	}
	g.Pause()
	if !g.Paused() {
		t.Fatal("Pause 后应暂停态")
	}
	// Wait 应阻塞
	done := make(chan error, 1)
	go func() { done <- g.Wait(context.Background()) }()
	select {
	case <-done:
		t.Fatal("暂停态 Wait 不应返回")
	case <-time.After(50 * time.Millisecond):
	}
	g.Resume()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("恢复后 Wait 返回错误: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Resume 后 Wait 未返回")
	}
	if g.Paused() {
		t.Fatal("Resume 后应运行态")
	}
}

func TestGateWaitCancel(t *testing.T) {
	g := NewGate()
	g.Pause()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- g.Wait(ctx) }()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("ctx 取消时 Wait 应返回错误")
		}
	case <-time.After(time.Second):
		t.Fatal("ctx 取消后 Wait 未返回")
	}
}

func TestStateMachineMatrix(t *testing.T) {
	// 全部 (from,to) 组合的合法性矩阵（04 M1-T09 DoD：状态转换单测全覆盖）
	legal := map[model.TaskStatus]map[model.TaskStatus]bool{
		model.StatusIdle:         {model.StatusScanning: true},
		model.StatusScanning:     {model.StatusPrefiltering: true, model.StatusPaused: true, model.StatusCancelled: true, model.StatusFailed: true},
		model.StatusPrefiltering: {model.StatusHashing: true, model.StatusPaused: true, model.StatusCancelled: true, model.StatusFailed: true},
		model.StatusHashing:      {model.StatusDone: true, model.StatusPaused: true, model.StatusCancelled: true, model.StatusFailed: true},
		model.StatusPaused:       {model.StatusScanning: true, model.StatusPrefiltering: true, model.StatusHashing: true, model.StatusCancelled: true},
		model.StatusDone:         {model.StatusIdle: true},
		model.StatusCancelled:    {model.StatusIdle: true},
		model.StatusFailed:       {model.StatusIdle: true},
	}
	statuses := []model.TaskStatus{
		model.StatusIdle, model.StatusScanning, model.StatusPrefiltering,
		model.StatusHashing, model.StatusDone, model.StatusPaused,
		model.StatusCancelled, model.StatusFailed,
	}
	for _, from := range statuses {
		for _, to := range statuses {
			got := model.ValidateTransition(from, to)
			want := legal[from][to]
			if got != want {
				t.Errorf("转换 %s → %s: got=%v want=%v", from, to, got, want)
			}
		}
	}
}
