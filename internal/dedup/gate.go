// Gate 暂停/恢复闸门（01 §5.5 暂停语义：停止派发新任务，in-flight 完成后挂起）。
package dedup

import (
	"context"
	"sync"
)

// Gate 暂停/恢复控制。
type Gate struct {
	mu   sync.Mutex
	wait chan struct{} // 非 nil 表示暂停中；Resume 时 close 唤醒
}

// NewGate 创建运行态闸门。
func NewGate() *Gate { return &Gate{} }

// Wait 阻塞直到运行或 ctx 结束。返回 ctx 错误表示应退出。
func (g *Gate) Wait(ctx context.Context) error {
	g.mu.Lock()
	if g.wait == nil {
		g.mu.Unlock()
		return nil
	}
	ch := g.wait
	g.mu.Unlock()
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Pause 暂停：后续 Wait 将阻塞至 Resume。
func (g *Gate) Pause() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.wait == nil {
		g.wait = make(chan struct{})
	}
}

// Resume 恢复运行。
func (g *Gate) Resume() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.wait != nil {
		close(g.wait)
		g.wait = nil
	}
}

// Paused 当前是否处于暂停态。
func (g *Gate) Paused() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.wait != nil
}
