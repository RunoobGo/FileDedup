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
	// M145（第 3 轮 §24.4 PRG-3）：改前这句是 `ev.ETASeconds != 0 && ev.ETASeconds != -1`，
	// 其中 `!= 0` 恒不成立——推导全在 progress.go 里：ETASeconds 只在 `if elapsed > 0`
	// 块内被写（:152），而本用例没调 Start ⇒ snapshotLocked 的 :129-131 把零值 start
	// 置为 now ⇒ :140 `elapsed = now.Sub(start).Seconds()` 恰为 0，那条写入根本不执行，
	// 字段原样带着 :138 的初值 -1 交回。于是「0」成了一个恒被放过的值：
	// 按 §24.4 的红法给 elapsed==0 那条路补一句 `ev.ETASeconds = 0`，改前整包照绿
	// （实得读数见本轮记录），判据对那一格是瞎的。
	// 收紧为只认 -1（未知）——**这是补强不是放宽**（约束 2 管的是反方向），
	// 相邻几条断言一字不动。
	if ev.ETASeconds != -1 {
		t.Fatalf("未启动计时时 ETA 只能是初值 -1（未知）：实得 %d。"+
			"elapsed==0 那条路没有任何写入点，交出别的值即说明 ETA 被无中生有地算了出来", ev.ETASeconds)
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

// TestTrackerETAWithOvercountedBytes 钉住 M14：BytesDone 瞬时超过 BytesTotal 时
// （阶段切换重设总量、缓存命中回填、重试重复计数都会造成这一瞬时），
// `ev.BytesTotal - ev.BytesDone` 在 **uint64 域先减**再转 float64，
// 回绕成 ~1.8e19，下一行的 `remain < 0` 因此是死代码，ETA 直接爆成天文数字。
func TestTrackerETAWithOvercountedBytes(t *testing.T) {
	tr := New(time.Hour, nil)
	tr.Start(context.Background())
	tr.SetTotal(0, 1000)
	tr.AddBytes(1200) // 完成量超过总量：进度条语义上就是"已到头"
	time.Sleep(50 * time.Millisecond)

	ev := tr.Stop()
	if ev.ETASeconds != 0 {
		t.Fatalf("瞬时超额时 ETA 应夹到 0，实得 %d（uint64 先减回绕，M14 未修）", ev.ETASeconds)
	}
}
