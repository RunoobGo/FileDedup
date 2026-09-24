package ops

import (
	"sync"
	"testing"
	"time"
)

// M197（2026-09-24，登记 §6.29，裁定「整体加互斥」）：回收站调用的串行列本体。
//
// 本格只钉"排队是否真发生"这一条判据——它在任何平台都可跑，故不挂 build tag。
// ★ 不钉的那一格：windows 的 `defaultTrash` 是否真接了这条列，以及"并发腿的回收站
// 计数增量互相污染"这个缺陷本体是否被关闭。前者靠交叉 vet + 现读接线，后者需要真实
// 回收站的计数推进（darwin 造不出来），已登记 docs/05 W1-10 走真机。

// TestWithTrashSerializesConcurrentCalls 8 路并发进临界区：任意时刻在栏内的工作者
// 数必须恒为 1；且每个参数都要被送达一次（串行 ≠ 丢单）。
func TestWithTrashSerializesConcurrentCalls(t *testing.T) {
	const n = 8

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		inFlight  int
		maxFlight int
		seen      = map[string]int{}
	)

	call := func(paths []string) (map[string]string, error) {
		mu.Lock()
		inFlight++
		if inFlight > maxFlight {
			maxFlight = inFlight
		}
		seen[paths[0]]++
		mu.Unlock()

		// 让重叠窗口有实际可观测的时长：无串行时 8 路几乎必然同时在栏
		// （本用例启动 8 路的间隔远小于这个睡眠，摘掉锁即稳定复现 maxFlight>1）。
		time.Sleep(20 * time.Millisecond)

		mu.Lock()
		inFlight--
		mu.Unlock()
		return map[string]string{paths[0]: "dst"}, nil
	}

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			dst, err := withTrashSerial(call, []string{string(rune('a' + i))})
			if err != nil || dst[string(rune('a'+i))] != "dst" {
				t.Errorf("第 %d 路：串行列丢单或错投（err=%v dst=%v）", i, err, dst)
			}
		}(i)
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if maxFlight > 1 {
		t.Errorf("串行列未生效：临界区内最多同时有 %d 路（要求恒为 1）——"+
			"回收站计数增量判据会被并发腿互相污染", maxFlight)
	}
	if len(seen) != n {
		t.Errorf("参数集不完整：应送达 %d 个不同参数，实到 %d 个（%v）", n, len(seen), seen)
	}
	for p, c := range seen {
		if c != 1 {
			t.Errorf("参数 %q 被调用 %d 次（应为 1）", p, c)
		}
	}
}

// TestWithTrashReleasesLockOnPanic 列必须在被调方失败/panic 时照旧放行下一腿：
// 锁泄漏的形态是"整条回收站路永久卡死"，比原判据缺陷更糟。
func TestWithTrashReleasesLockOnPanic(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("预期的 panic 未发生，本用例没有测到解锁面")
			}
			close(done)
		}()
		withTrashSerial(func([]string) (map[string]string, error) {
			panic("boom")
		}, nil)
	}()
	<-done

	// 若 panic 时没解锁，这一格会永久拿不到锁（-race 与 CI 都会挂在超时上，而非静默）。
	withTimeout := make(chan struct{})
	go func() {
		withTrashSerial(func([]string) (map[string]string, error) {
			return map[string]string{}, nil
		}, nil)
		close(withTimeout)
	}()
	select {
	case <-withTimeout:
	case <-time.After(5 * time.Second):
		t.Error("panic 后串行列没有放行下一腿：锁泄漏")
	}
}
