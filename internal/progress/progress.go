// Package progress 进度统计：原子计数 + 500ms 节流回调（04 M1-T10）。
package progress

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"filededup/internal/model"
)

// Tracker 进度跟踪器。引擎层回调接口，不绑定 Wails。
type Tracker struct {
	stage      atomic.Value // string
	filesDone  atomic.Uint64
	bytesDone  atomic.Uint64
	filesTotal atomic.Uint64
	bytesTotal atomic.Uint64

	mu       sync.Mutex
	start    time.Time
	lastEmit time.Time
	cb       func(model.ProgressEvent)
	interval time.Duration
	stopCh   chan struct{}
	stopOnce sync.Once
}

// New 创建 Tracker。interval 为节流间隔（生产 500ms，测试可调小）。
func New(interval time.Duration, cb func(model.ProgressEvent)) *Tracker {
	t := &Tracker{
		interval: interval,
		cb:       cb,
		stopCh:   make(chan struct{}),
	}
	if t.interval <= 0 {
		t.interval = 500 * time.Millisecond
	}
	t.stage.Store("scan")
	return t
}

// Start 启动节流推送 goroutine（阻塞前请先调用）。
func (t *Tracker) Start(ctx context.Context) {
	t.mu.Lock()
	t.start = time.Now()
	t.mu.Unlock()
	go func() {
		ticker := time.NewTicker(t.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.stopCh:
				return
			case <-ticker.C:
				t.mu.Lock()
				now := time.Now()
				t.lastEmit = now
				ev := t.snapshotLocked(now)
				cb := t.cb
				t.mu.Unlock()
				if cb != nil {
					cb(ev)
				}
			}
		}
	}()
}

// Stop 停止推送并返回终值；若设置回调则先推送终值（"节流不丢终值"）。
func (t *Tracker) Stop() model.ProgressEvent {
	t.stopOnce.Do(func() { close(t.stopCh) })
	t.mu.Lock()
	now := time.Now()
	t.lastEmit = now
	ev := t.snapshotLocked(now)
	cb := t.cb
	t.mu.Unlock()
	if cb != nil {
		cb(ev)
	}
	return ev
}

// SetStage 切换阶段。
func (t *Tracker) SetStage(s string) { t.stage.Store(s) }

// SetTotal 设置总量预估值（0 = 未知）。
func (t *Tracker) SetTotal(files, bytes uint64) {
	t.filesTotal.Store(files)
	t.bytesTotal.Store(bytes)
}

// AddFile 计入完成文件数。
func (t *Tracker) AddFile() { t.filesDone.Add(1) }

// AddBytes 计入完成字节数。
func (t *Tracker) AddBytes(n uint64) { t.bytesDone.Add(n) }

// Snapshot 主动拉取当前进度（GetScanProgress 语义）。
func (t *Tracker) Snapshot() model.ProgressEvent {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.snapshotLocked(time.Now())
}

func (t *Tracker) snapshotLocked(now time.Time) model.ProgressEvent {
	start := t.start
	if start.IsZero() {
		start = now
	}
	ev := model.ProgressEvent{
		Stage:      t.stage.Load().(string),
		FilesDone:  t.filesDone.Load(),
		FilesTotal: t.filesTotal.Load(),
		BytesDone:  t.bytesDone.Load(),
		BytesTotal: t.bytesTotal.Load(),
		ETASeconds: -1,
	}
	elapsed := now.Sub(start).Seconds()
	if elapsed > 0 {
		ev.SpeedBps = float64(ev.BytesDone) / elapsed
		if ev.BytesTotal > 0 && ev.SpeedBps > 0 {
			remain := float64(ev.BytesTotal - ev.BytesDone)
			if remain < 0 {
				remain = 0
			}
			ev.ETASeconds = int64(remain / ev.SpeedBps)
		}
	}
	return ev
}
