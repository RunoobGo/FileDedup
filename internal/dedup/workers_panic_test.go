package dedup

// ② panic 收口回归：runWorkers 内 worker panic 必须被拦截（进程存活）、
// 经 onPanic 上报（供上层置 workerPanic 标记 → Failed 而非 Cancelled），
// 且 wg 正常返回（不得挂死其余协作路径）。

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRunWorkersPanicContainedAndReported(t *testing.T) {
	var mu sync.Mutex
	var msgs []string
	runWorkers(context.Background(), 4, func(m string) {
		mu.Lock()
		msgs = append(msgs, m)
		mu.Unlock()
	}, func() error {
		panic("boom")
	})
	// 执行到此处即证明 panic 未杀死进程、wg 未挂死
	mu.Lock()
	defer mu.Unlock()
	if len(msgs) != 4 {
		t.Fatalf("onPanic 调用次数 = %d, want 4（每个 worker 各 panic 一次）", len(msgs))
	}
	for _, m := range msgs {
		if !strings.Contains(m, "boom") {
			t.Errorf("onPanic 消息缺少 panic 值: %q", m)
		}
	}
}

func TestRunWorkersPanicNilReporterSafe(t *testing.T) {
	// onPanic 为 nil 时 panic 仍须被拦截（不得二次 panic 于 nil 回调）
	done := make(chan struct{})
	go func() {
		defer close(done)
		runWorkers(context.Background(), 2, nil, func() error {
			panic("nil-reporter")
		})
	}()
	<-done
}

func TestRunWorkersHealthyPathUnaffected(t *testing.T) {
	var ran atomic.Int64
	runWorkers(context.Background(), 3, func(string) {
		t.Error("健康路径不应触发 onPanic")
	}, func() error {
		ran.Add(1)
		return nil
	})
	if got := ran.Load(); got != 3 {
		t.Fatalf("fn 执行次数 = %d, want 3", got)
	}
}
