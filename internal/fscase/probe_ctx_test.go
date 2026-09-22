package fscase

// R2-2（Task B2，设计段 §30.4）：卷语义探测必须有一条**能松手的路**。
//
// 改前的形状（现状读数见设计段，不在这里复述）：`Verdict` 在 goroutine 之外同步跑 `probe`，
// 而 `probe` 的三个 syscall（OpenFile / Lstat / Remove）一个都不读 ctx ⇒ 死挂载
// （NFS 硬挂载、拔走的 SMB）上任意一次挂住 = 上层那趟扫描**永久**停在 Scanning，
// `Pipeline.Cancel` 只是 cancel 一个没人读的 ctx。AS-R3 修的是 app 层持锁探测，
// 这一腿是同族第二处，此前零登记。
//
// ★ 本文件钉的是三件事，缺一件就是假绿：
//   ① 取消/超时能让 `VerdictCtx` 及时返回，并如实带上 ctx 的错误；
//   ② 被取消那一格**不进缓存**（M126：一次瞬时故障不许钉死整趟扫描），
//      松手后同一目录仍要拿得到实测结论；
//   ③ `Verdict` 与 `VerdictCtx(Background)` 同源（一份判定，不留第二份实现）。
//
// 夹具用 `SetProbeHook`（AS-R3 留下的观察点）扮演"卡在 syscall 里的探测"：钩子在 `probe`
// 动手**之前**同步回调，卡住它等价于卡住那次写盘，因此本批**不新增**生产接缝。

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// blockingProbeHook 装一个"进钩子即报到、然后停在那里"的探测。
// 返回值：entered 在第一次探测进门时关闭一次；release 松手（幂等）。
func blockingProbeHook(t *testing.T) (entered <-chan struct{}, release func()) {
	t.Helper()
	enter := make(chan struct{})
	stopped := make(chan struct{})
	var markOnce, stopOnce sync.Once
	SetProbeHook(func(string) {
		markOnce.Do(func() { close(enter) })
		<-stopped
	})
	release = func() { stopOnce.Do(func() { close(stopped) }) }
	t.Cleanup(func() {
		SetProbeHook(nil)
		release()
	})
	return enter, release
}

// cachedVerdict 直接查按目录缓存的表（同包测试特权，不是生产接缝）。
// ②那一格的唯一观测面就是这张表：取消后表里必须**没有**这一行。
func cachedVerdict(key string) (Result, bool) {
	mu.Lock()
	defer mu.Unlock()
	v, ok := cache[key]
	return v, ok
}

type verdictCall struct {
	v   Result
	err error
}

// callVerdictCtx 在 goroutine 里问一次，配合"探测已被卡住"的现场使用。
func callVerdictCtx(ctx context.Context, dir string) <-chan verdictCall {
	ch := make(chan verdictCall, 1) // 带缓冲：断言失败时被放弃的这一次不许卡在发送上
	go func() {
		v, err := VerdictCtx(ctx, dir)
		ch <- verdictCall{v, err}
	}()
	return ch
}

func TestVerdictCtxAbortsBlockedProbe(t *testing.T) {
	dir := t.TempDir()
	entered, release := blockingProbeHook(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := callVerdictCtx(ctx, dir)
	<-entered // 前提自检：探测确实已经进门（被卡在 syscall 里），否则本条测不到东西

	cancel()
	var got verdictCall
	select {
	case got = <-ch:
	case <-time.After(3 * time.Second):
		release()
		t.Fatalf("取消后 3s 内 VerdictCtx 没有返回：探测仍然不可中断（R2-2 的缺口原样存在）")
	}
	if !errors.Is(got.err, context.Canceled) {
		release()
		t.Fatalf("取消那一格必须交回 ctx 的错误，实得 err=%v", got.err)
	}
	if got.v != (Result{}) {
		release()
		t.Fatalf("取消那一格必须交回零值结论，不许给一份猜测：%+v", got.v)
	}
	if v, ok := cachedVerdict(filepath.Clean(dir)); ok {
		release()
		t.Fatalf("被取消的探测把结论钉进了按目录缓存（M126：一次瞬时故障不许钉死整趟扫描）：%+v", v)
	}
	release()

	// ★ 松手之后同一目录仍要拿得到实测结论——上面那条"不进缓存"的真正兑现面。
	if v := Verdict(dir); !v.Proven {
		t.Fatalf("取消过一次之后该目录永久拿不到实测结论：%+v", v)
	}
}

func TestVerdictCtxHonorsDeadline(t *testing.T) {
	dir := t.TempDir()
	entered, release := blockingProbeHook(t)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	ch := callVerdictCtx(ctx, dir)
	<-entered

	select {
	case got := <-ch:
		if !errors.Is(got.err, context.DeadlineExceeded) {
			release()
			t.Fatalf("超时那一格必须交回 DeadlineExceeded，实得 err=%v", got.err)
		}
		if got.v != (Result{}) {
			release()
			t.Fatalf("超时不许换成一份猜测：%+v", got.v)
		}
	case <-time.After(3 * time.Second):
		release()
		t.Fatalf("deadline 不能让 VerdictCtx 返回：超时只能由调用方的 ctx 承载，本包不另设常量")
	}
	if _, ok := cachedVerdict(filepath.Clean(dir)); ok {
		release()
		t.Fatalf("超时被钉进了缓存：%+v", mustGet(t, filepath.Clean(dir)))
	}
	release()
}

func mustGet(t *testing.T, key string) Result {
	t.Helper()
	v, _ := cachedVerdict(key)
	return v
}

// 缓存命中的那一格必须在 ctx 之前生效：已经实测过的结论不该被一次取消抹掉。
// ★ 这条同时钉住"缓存仍然在挡探测"——R2-2 把探测搬进 goroutine 后，
// 若哪天有人把查表搬到 select 之后，每次问卷都往用户目录写一个探测文件。
func TestVerdictCtxServesCacheWithoutProbing(t *testing.T) {
	dir := t.TempDir()
	if v := Verdict(dir); !v.Proven {
		t.Fatalf("夹具前提不成立：可写目录上首问就该确证，实得 %+v", v)
	}
	SetProbeHook(func(string) { t.Errorf("命中缓存却仍在探测（缓存被绕过）") })
	t.Cleanup(func() { SetProbeHook(nil) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 已取消：命中缓存时这一次仍然该给结论
	got := <-callVerdictCtx(ctx, dir)
	if got.err != nil {
		t.Fatalf("已有实测结论时被 ctx 取消抹掉了：err=%v", got.err)
	}
	if !got.v.Proven {
		t.Fatalf("缓存里那一格丢了：%+v", got.v)
	}
}

// ③ 同源：Verdict 是 VerdictCtx(Background) 的薄壳，两处必须给同一个答案。
// 与既有的 TestSensitiveIsBoolViewOfVerdict 同一手法（一个可写目录 + 一个探针必失败的目录）。
func TestVerdictIsVerdictCtxOnBackground(t *testing.T) {
	for _, dir := range []string{t.TempDir(), missingProbeDir(t)} {
		want, err := VerdictCtx(context.Background(), dir)
		if err != nil {
			t.Fatalf("Background 永不到期，这一格不该有错误：%v", err)
		}
		if got := Verdict(dir); got != want {
			t.Fatalf("Verdict(%q)=%+v 与 VerdictCtx(Background)=%+v 不同源", dir, got, want)
		}
	}
}
