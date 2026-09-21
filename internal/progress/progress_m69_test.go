package progress

// M69（04 §6.11 登记表 PROGRESS-1）探针：设计段 §18.1-5 / §18.3 P-18-6。
//
// 要钉的性质只有一句：**Stop 返回之后，不得再有更早的快照被外发**。
// 既有 TestTrackerThrottle 只断言"终值包含全部计数"，没测顺序，所以窗口一直在那儿。

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"filededup/internal/model"
)

// hold 是给"被卡在 cb 里的那条旧快照"的放行时限。取值只需大于主 goroutine
// 走完 AddFile+Stop 的时间（微秒级），留 60ms 是给 -race 与机器负载的余量。
func TestStopNeverFollowedByOlderSnapshot(t *testing.T) {
	const hold = 60 * time.Millisecond

	var (
		mu        sync.Mutex
		seq       []uint64
		entered   = make(chan struct{})
		firstDone = make(chan struct{})
		onEntry   atomic.Int64
		release   = make(chan struct{})
	)
	cb := func(ev model.ProgressEvent) {
		if onEntry.Add(1) == 1 { // 第一条外发（必是 ticker 那条）先卡住，复现"已取快照、尚未发出"的窗口
			close(entered)
			select {
			case <-release:
			case <-time.After(hold):
			}
			defer close(firstDone)
		}
		mu.Lock()
		seq = append(seq, ev.FilesDone)
		mu.Unlock()
	}

	tr := New(time.Millisecond, cb)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr.Start(ctx)

	<-entered // 此刻 ticker 已取到快照（FilesDone=0），正卡在锁外的 cb 里
	tr.AddFile()
	tr.AddFile()
	tr.AddFile()
	end := tr.Stop()
	close(release)
	// 等那条被卡住的外发真的落进序列，再看顺序。不等就取证会把"改前的顺序错"
	// 读成"外发不足两条"（第一版就是这样，断言根本没跑到顺序那一格）。
	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("被卡住的那条外发两秒内没回来：夹具失效")
	}

	mu.Lock()
	got := append([]uint64(nil), seq...)
	mu.Unlock()
	if len(got) < 2 {
		t.Fatalf("外发序列不足两条，测不到顺序：%v", got)
	}
	var max uint64
	for _, v := range got {
		if v > max {
			max = v
		}
	}
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Errorf("Stop 之后又外发了更旧的快照（进度倒退）：序列 %v", got)
			break
		}
	}
	if got[len(got)-1] != max {
		t.Errorf("界面看到的最后一条必须是最大值：末值=%d 最大=%d 序列=%v", got[len(got)-1], max, got)
	}
	if end.FilesDone != max {
		t.Errorf("Stop 返回值也必须是终值：%d vs %d", end.FilesDone, max)
	}
}
