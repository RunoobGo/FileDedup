package progress

import (
	"context"
	"sync"
	"testing"
	"time"

	"filededup/internal/model"
)

func TestTrackerCounts(t *testing.T) {
	tr := New(time.Hour, nil) // 不启动推送
	tr.SetStage("hash")
	tr.SetTotal(100, 1000)
	for i := 0; i < 100; i++ {
		tr.AddFile()
		tr.AddBytes(10)
	}
	ev := tr.Snapshot()
	if ev.Stage != "hash" || ev.FilesDone != 100 || ev.BytesDone != 1000 {
		t.Fatalf("快照错误: %+v", ev)
	}
	if ev.FilesTotal != 100 || ev.BytesTotal != 1000 {
		t.Fatalf("总量错误: %+v", ev)
	}
	if ev.ETASeconds != 0 && ev.ETASeconds != -1 {
		t.Fatalf("未启动计时时 ETA 应为 0 或 -1: %d", ev.ETASeconds)
	}
}

func TestTrackerThrottle(t *testing.T) {
	// 节流：10ms 间隔内 1000 次计数只触发少量回调；Stop 终值包含全部计数
	var mu sync.Mutex
	var events []model.ProgressEvent
	tr := New(50*time.Millisecond, func(ev model.ProgressEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr.Start(ctx)
	for i := 0; i < 1000; i++ {
		tr.AddFile()
	}
	final := tr.Stop()
	if final.FilesDone != 1000 {
		t.Fatalf("终值丢失: %d", final.FilesDone)
	}
	mu.Lock()
	n := len(events)
	mu.Unlock()
	if n > 5 {
		t.Fatalf("节流失效: %d 次回调（预期 ≤5）", n)
	}
}

func TestTrackerETA(t *testing.T) {
	tr := New(time.Hour, nil)
	tr.Start(context.Background())
	tr.SetTotal(0, 1000)
	tr.AddBytes(500)
	time.Sleep(50 * time.Millisecond)
	ev := tr.Stop()
	if ev.SpeedBps <= 0 {
		t.Fatalf("速度未计算: %+v", ev)
	}
	if ev.ETASeconds < 0 {
		t.Fatalf("已知总量时 ETA 不应为 -1: %+v", ev)
	}
}
