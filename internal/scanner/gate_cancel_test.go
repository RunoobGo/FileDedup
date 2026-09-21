package scanner

// ② 取消/暂停收口回归：WalkWithGate 的 gate 在每次取目录前生效（暂停即停遍历），
// ctx 取消后 worker 必须在 ReadDir 之前 drain（不再产生磁盘 I/O、快速返回）。

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"filededup/internal/model"
)

// testGate 可注入的 Waiter：pause 后 Wait 阻塞，resume 放行全部等待者。
type testGate struct {
	mu     sync.Mutex
	resume chan struct{} // 非 nil = 暂停中
}

func (g *testGate) Pause() {
	g.mu.Lock()
	if g.resume == nil {
		g.resume = make(chan struct{})
	}
	g.mu.Unlock()
}

func (g *testGate) Resume() {
	g.mu.Lock()
	ch := g.resume
	g.resume = nil
	g.mu.Unlock()
	if ch != nil {
		close(ch)
	}
}

func (g *testGate) Wait(ctx context.Context) error {
	for {
		g.mu.Lock()
		ch := g.resume
		g.mu.Unlock()
		if ch == nil {
			return ctx.Err()
		}
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func makeDeepTree(t *testing.T, dirs, filesPerDir int) string {
	t.Helper()
	root := t.TempDir()
	for d := 0; d < dirs; d++ {
		dir := filepath.Join(root, "d"+strconv.Itoa(d))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < filesPerDir; i++ {
			p := filepath.Join(dir, "f"+strconv.Itoa(i)+".txt")
			if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}

// TestWalkWithGatePauseResume：暂停期间不得有新目录被访问；恢复后完整收齐文件。
func TestWalkWithGatePauseResume(t *testing.T) {
	root := makeDeepTree(t, 8, 4)
	gate := &testGate{}
	gate.Pause()

	done := make(chan *Result, 1)
	go func() {
		done <- WalkWithGate(context.Background(), []string{root}, &model.Filters{}, 4, gate)
	}()

	select {
	case <-done:
		t.Fatal("暂停期间 Walk 不应完成")
	case <-time.After(150 * time.Millisecond):
	}

	gate.Resume()
	select {
	case res := <-done:
		if len(res.Files) != 32 {
			t.Fatalf("恢复后收齐文件数 = %d, want 32", len(res.Files))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("恢复后 Walk 未在 10s 内完成")
	}
}

// TestWalkWithGateCancelDrainsWithoutIO：取消后立刻返回；
// 取消发生在任何目录处理之前时，不得收齐全部文件（drain 路径零磁盘 I/O）。
func TestWalkWithGateCancelDrainsWithoutIO(t *testing.T) {
	root := makeDeepTree(t, 60, 2)
	ctx, cancel := context.WithCancel(context.Background())
	gate := &testGate{}
	// 先暂停，保证取消发生在任何目录被处理之前（确定性地测 drain 行为）
	gate.Pause()

	done := make(chan *Result, 1)
	go func() {
		done <- WalkWithGate(ctx, []string{root}, &model.Filters{}, 4, gate)
	}()
	time.Sleep(50 * time.Millisecond) // 让 worker 停在 gate.Wait
	cancel()
	gate.Resume() // 解除暂停，让被唤醒的 Wait 走 ctx.Done 分支

	var res *Result
	select {
	case res = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("取消后 Walk 未在 5s 内返回")
	}
	// M67 后的**补强**（设计段 §19.3 P-19-5，就地论证，不是放松）：
	// 原句是 `if len(res.Files) == 120 && res.Visited > 0`，那个 `Visited > 0` 合取
	// 钉的正是被 M67 推翻的旧口径（visited 当去重集 ⇒ 取消排空也至少报 1）。
	// 新口径下取消排空恒为 0，于是整条合取**永不成立** —— 用例变成空转的假绿。
	// 现在拆成两格：files 收齐 120 直接判失败（不再要求 visited 陪证），
	// visited==0 另作"零磁盘 I/O"的正证。前者在改前也会红（files=0 时不触发，
	// 触发条件是行为真的复发），后者由 scanner_m67_test.go 的 P-19-5 取到修前红。
	if len(res.Files) == 120 {
		t.Errorf("取消后仍完成了全部遍历（应快速 drain）: files=%d visited=%d", len(res.Files), res.Visited)
	}
	if res.Visited != 0 {
		t.Errorf("取消排空阶段不得有任何一次成功的 ReadDir，实测 Visited=%d", res.Visited)
	}
}
