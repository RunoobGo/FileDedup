package scanner

// R2-2（Task B2，设计段 §30.4）：**扫描启动腿必须能被一次卡住的探测松手**。
//
// 改前的形状：`WalkWithGate` 在 worker 启动**之前**同步跑 `dedupeRoots` 那一圈卷语义探测，
// 而探测（`fscase.Verdict` → `probe` 的 OpenFile/Lstat/Remove）一个 ctx 都不读 ⇒
// 死挂载（NFS 硬挂载、拔走的 SMB）上任一次挂住 = 扫描永久停在 Scanning，
// `Pipeline.Cancel` 只是 cancel 一个没人读的 ctx（`dedup/pipeline.go:351` 那个取消检查
// 在 `WalkWithGate` 返回之后才执行，永远执行不到）。
//
// ★ 本文件钉两格，缺一格就是假绿：
//   ① 探测被中断 ⇒ `WalkWithGate` **及时返回**（不是等 syscall 自己松手）；
//   ② 及时返回之后**不许带着没问全的折叠继续扫**：退默认在 darwin/windows 等于"不敏感"
//     （fscase/default_insensitive.go），那是一棵真不同的树整棵静默不漏扫的形状，
//     比挂死更难发现。
//
// 两格都用注入缝 `probeCaseVerdict`（H6 惯例）扮演现场，不碰真卷、不 sleep 等运气。

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"filededup/internal/fscase"
	"filededup/internal/model"
)

// TestWalkWithGateAbortsPromptlyWhenProbeIsCancelled 钉 ①：探测卡在 syscall 里时，
// 取消必须能让扫描启动这条腿返回。
func TestWalkWithGateAbortsPromptlyWhenProbeIsCancelled(t *testing.T) {
	roots := siblingRoots(t) // 三根 ⇒ 一定走探测那一圈
	restoreProbeSeam(t)
	var probed atomic.Int32
	probeCaseVerdict = func(ctx context.Context, _ string) (fscase.Result, error) {
		probed.Add(1)
		<-ctx.Done() // 扮演"卡在 syscall 里、直到被松手才返回"的探测
		return fscase.Result{}, ctx.Err()
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 取消落在探测**之前**：真实现场是落在探测中间，两者对这条腿的要求同一格
	// ——探测必须观察调用方的 ctx。取最早的那一格，是为了不用 sleep 碰运气。
	done := make(chan *Result, 1)
	go func() { done <- WalkWithGate(ctx, roots, &model.Filters{}, 4, nil) }()

	select {
	case res := <-done:
		if res.Visited != 0 || len(res.Files) != 0 {
			t.Fatalf("扫描在探测被中断后照样走完了（Visited=%d files=%d）：折叠判据没问全就不该开扫",
				res.Visited, len(res.Files))
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("取消后 3s 内 WalkWithGate 没有返回：探测仍然不可中断（R2-2 的缺口原样存在）。" +
			"\n      负控制验证：把 dedupeRoots 传给 probeCaseVerdict 的 ctx 换成 context.Background()，必须红在这里。")
	}
	if n := probed.Load(); n == 0 {
		t.Fatalf("前提自检失败：这条腿上一次探测都没发生（探测数为 0 ⇒ 本条什么都没测到）")
	}
}

// TestWalkWithGateDoesNotScanWithIncompleteRoots 钉 ②：探测报错时 ctx 可以**没被取消**
// （今天唯一的生产来源是取消，但契约是"探测交回错误 ⇒ 本轮不扫"，用它自己的形状测）。
// 删掉 WalkWithGate 里那道 `err != nil` 收口，本条必须红在"照样扫出了文件"。
func TestWalkWithGateDoesNotScanWithIncompleteRoots(t *testing.T) {
	roots := siblingRoots(t)
	restoreProbeSeam(t)
	probeCaseVerdict = func(context.Context, string) (fscase.Result, error) {
		return fscase.Result{}, errors.New("探测被中断（扮演）")
	}
	if n := probeCalls(t, roots); n == 0 {
		t.Fatalf("前提自检失败：探测一次都没发起（n=%d）", n)
	}

	res := WalkWithGate(context.Background(), roots, &model.Filters{}, 4, nil)
	if res.Visited != 0 || len(res.Files) != 0 {
		t.Fatalf("带着没问全的折叠继续扫了：Visited=%d files=%d unproven=%d"+
			"（退默认=darwin/windows 的\"不敏感\"，那时每一根的下标都可能指错卷）",
			res.Visited, len(res.Files), res.CaseProbeUnproven)
	}
	if res.CaseProbeUnproven != 0 {
		t.Fatalf("被中断的这一轮不该给出任何卷语义计数：unproven=%d", res.CaseProbeUnproven)
	}
}

// probeCalls 单独跑一趟 WalkWithGate 数探测发起次数（上一条的前提自检用）。
func probeCalls(t *testing.T, roots []string) int32 {
	t.Helper()
	var n atomic.Int32
	inner := probeCaseVerdict
	probeCaseVerdict = func(ctx context.Context, dir string) (fscase.Result, error) {
		n.Add(1)
		return inner(ctx, dir)
	}
	_ = WalkWithGate(context.Background(), roots, &model.Filters{}, 4, nil)
	return n.Load()
}

// TestDedupeRootsPropagatesProbeError 把契约钉在被改的那个函数上：错误必须从
// dedupeRoots 交出来，而不是在函数内部被折成"退默认继续"。
func TestDedupeRootsPropagatesProbeError(t *testing.T) {
	roots := siblingRoots(t)
	restoreProbeSeam(t)
	want := errors.New("探测被中断（扮演）")
	probeCaseVerdict = func(context.Context, string) (fscase.Result, error) {
		return fscase.Result{}, want
	}
	kept, _, _, verdicts, err := dedupeRoots(context.Background(), roots, false)
	if !errors.Is(err, want) {
		t.Fatalf("dedupeRoots 把探测的错误吞了：err=%v want=%v", err, want)
	}
	if verdicts != nil {
		t.Fatalf("出错那一趟仍交回了卷读数（调用点无法分辨真假）：%+v", verdicts)
	}
	// 根集合本身是纯字符串工作的产物，出错时照样交全：调用点据此才能报"哪些根没扫"。
	if len(kept) != len(roots) {
		t.Fatalf("出错时交回的规范化根数 = %d, want %d", len(kept), len(roots))
	}
}
