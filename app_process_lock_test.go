package main

// AS-R3（2026-09-20 全仓审计）：PreviewProcessPolicy 的注释写着
// "纯内存计算（无 I/O）"，实际它在持 a.mu 的窗口里经
// ops.ApplyProcessPolicy → fscase.Sensitive 往**用户任意可写目录写探测文件**。
// 死挂载（网络盘/NFS 拔走后 stat 会挂死）时，这一句 I/O 会把 a.mu 连同
// 全部 Wails 绑定一起卡住——预览是只读操作，本来绝不该有这种威力。
//
// 观测点在 fscase.SetProbeHook：它在探测真正动手前回调。
// 回调发生在哪个上下文里，就说明了"这次 I/O 是在锁内还是锁外"做的。

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"filededup/internal/fscase"
	"filededup/internal/model"
)

// TestPreviewProcessPolicyDoesNotProbeUnderLock 锁内不得有卷语义探测。
func TestPreviewProcessPolicyDoesNotProbeUnderLock(t *testing.T) {
	a, _, _, _, _ := procFixture(t)
	// 必须用一个**没被探测过**的目录：fscase 按目录缓存结论，命中缓存就不写盘，
	// 那样本用例什么都没观测到却会绿——所以顺手把"确实触发了探测"钉成前置条件。
	dir := t.TempDir()

	probed := false
	heldDuringProbe := false
	fscase.SetProbeHook(func(d string) {
		if filepath.Clean(d) != filepath.Clean(dir) {
			return
		}
		probed = true
		if !a.mu.TryLock() {
			heldDuringProbe = true
			return
		}
		a.mu.Unlock()
	})
	t.Cleanup(func() { fscase.SetProbeHook(nil) })

	if _, err := a.PreviewProcessPolicy([]string{dir}, nil, nil); err != nil {
		t.Fatal(err)
	}
	fscase.SetProbeHook(nil)

	if !probed {
		t.Fatalf("目录 %q 没有触发卷语义探测，本用例什么都没观测到（夹具失效，不是通过）", dir)
	}
	if heldDuringProbe {
		t.Fatal("在持有 a.mu 期间做了写盘探测：死挂载会卡住全部应用绑定（AS-R3）。" +
			"敏感性必须在取锁之前解析好，锁内只做纯比较。")
	}

	// 探测文件不得留在用户目录里（残留会被下一次扫描当成候选文件）。
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".fdd-case-probe-") {
			t.Fatalf("探测文件残留: %s", filepath.Join(dir, e.Name()))
		}
	}
}

// TestWarmSensitivityIsCacheHitAfterWarm 探测结论按目录缓存：同一次预览最多
// 探测一次，第二次预览一次都不探。
//
// 这条守住的是修向的前提——"锁外解析一次、锁内纯比较"只在有缓存时才不重复付
// 写盘代价；若缓存被去掉，预览会在每次勾选变化时往用户目录写文件。
func TestWarmSensitivityIsCacheHitAfterWarm(t *testing.T) {
	a, _, _, _, _ := procFixture(t)
	dir := t.TempDir()

	probesOnce := func() int {
		n := 0
		fscase.SetProbeHook(func(d string) {
			if filepath.Clean(d) == filepath.Clean(dir) {
				n++
			}
		})
		defer fscase.SetProbeHook(nil)
		if _, err := a.PreviewProcessPolicy([]string{dir}, nil, nil); err != nil {
			t.Fatal(err)
		}
		return n
	}

	if got := probesOnce(); got != 1 {
		t.Fatalf("首次预览触发 %d 次探测，应为 1 次（结论按目录缓存，多探说明每次都重算）", got)
	}
	if got := probesOnce(); got != 0 {
		t.Fatalf("第二次预览触发 %d 次探测，应为 0 次（该目录已探测过）", got)
	}
}

// ---- R2-3（Task B3，设计段 §30.5）：执行侧是这一族的第三处漏网 ----
//
// 预览（AS-R3）与保留策略（APP-1）都已改成"锁外预热、锁内纯比较"，`ExecuteOperation`
// 里那次 `ops.ApplyProcessPolicy` 没有：它跑在 `a.opsRunning = true` **之后**。
// 后果不是卡住一把锁（那时 `a.mu` 已经放开），而是卡住整个操作面——
// 探测永不返回 ⇒ `resetOps` 永不执行 ⇒ `opsRunning` 永久为真 ⇒
// 此后每次清理（app.go:1823）与每次新扫描（app.go:611）都被拒，
// 而 `CancelOperation` 只 cancel 一个没人读的 `opCtx`，解不了这一步。

// TestExecuteOperationProbesBeforeOpsRunning 钉"探测发生在占互斥位之前"。
// 判据取的是探测那一刻的 `a.opsRunning`，不是任何时序巧合。
func TestExecuteOperationProbesBeforeOpsRunning(t *testing.T) {
	a, rec, _, insideDir, _ := procFixture(t)
	ids := idsInDir(t, a, insideDir)
	if len(ids.ids) == 0 {
		t.Fatalf("夹具前提不成立：%s 下没有非保留项", insideDir)
	}

	var probed, heldDuringProbe, runningDuringProbe bool
	fscase.SetProbeHook(func(d string) {
		if filepath.Clean(d) != filepath.Clean(insideDir) {
			return
		}
		probed = true
		// TryLock 而不是 Lock：探针若在锁内触发，Lock 会自锁死，红就变成挂死。
		if !a.mu.TryLock() {
			heldDuringProbe = true
			return
		}
		runningDuringProbe = a.opsRunning
		a.mu.Unlock()
	})
	t.Cleanup(func() { fscase.SetProbeHook(nil) })

	if _, err := a.ExecuteOperation(model.OpRequest{
		Kind: "trash", FileIDs: ids.ids, ProcessDirs: []string{insideDir},
	}); err != nil {
		t.Fatal(err)
	}
	waitOpsDone(t, rec)
	fscase.SetProbeHook(nil)

	if !probed {
		t.Fatalf("夹具前提不成立：%q 一次探测都没触发（结论命中缓存 ⇒ 本条什么都没观测到，不算通过）", insideDir)
	}
	if heldDuringProbe {
		t.Fatal("在持有 a.mu 期间做了写盘探测（AS-R3 同族）：死挂载会卡住全部应用绑定")
	}
	if runningDuringProbe {
		t.Fatal("卷语义探测发生在 opsRunning 置真之后（R2-3）：这一步卡住 = 此后一切清理与新扫描都被拒，" +
			"且 CancelOperation 解不了。修法：入口取锁之前 ops.WarmSensitivity，下面改用 ApplyProcessPolicyWith。")
	}
	// 正向读数（AS-K2：不红不等于对）：搬动之后操作确实跑完，范围内的文件被回收。
	for _, p := range ids.paths {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("范围内的文件应已被处理: %s err=%v", p, err)
		}
	}
}

// TestExecuteOperationBlockedProbeDoesNotWedgeApp 钉住那条后果本身：
// 探测被卡住的**整个窗口**里，应用都不该处于"清理操作执行中"。
func TestExecuteOperationBlockedProbeDoesNotWedgeApp(t *testing.T) {
	a, rec, _, insideDir, _ := procFixture(t)
	ids := idsInDir(t, a, insideDir)
	if len(ids.ids) == 0 {
		t.Fatalf("夹具前提不成立：%s 下没有非保留项", insideDir)
	}

	entered := make(chan struct{})
	released := make(chan struct{})
	var once, closeOnce sync.Once
	release := func() { closeOnce.Do(func() { close(released) }) }
	fscase.SetProbeHook(func(d string) {
		if filepath.Clean(d) != filepath.Clean(insideDir) {
			return
		}
		once.Do(func() { close(entered) })
		<-released // 扮演死挂载上那次永不返回的写盘
	})
	t.Cleanup(func() {
		fscase.SetProbeHook(nil)
		release()
	})

	errCh := make(chan error, 1)
	go func() {
		_, err := a.ExecuteOperation(model.OpRequest{
			Kind: "trash", FileIDs: ids.ids, ProcessDirs: []string{insideDir},
		})
		errCh <- err
	}()

	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("3s 内没有任何探测发生（夹具失效，不是通过）")
	}
	a.mu.Lock()
	running := a.opsRunning
	a.mu.Unlock()
	if running {
		t.Fatal("探测被卡住时 opsRunning 已为真（R2-3）：新扫描与新操作从这一刻起永久被拒，" +
			"resetOps 在那条永不返回的 I/O 之后，永远轮不到执行")
	}
	t.Log("正向读数：探测被卡住的窗口里 opsRunning=false，应用未被钉成「清理操作执行中」")

	release()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("松手之后 ExecuteOperation 没有返回：预热搬动把执行链接断了")
	}
	waitOpsDone(t, rec)
}
