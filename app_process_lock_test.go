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
	"testing"

	"filededup/internal/fscase"
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

	if _, err := a.PreviewProcessPolicy([]string{dir}, nil); err != nil {
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
		if _, err := a.PreviewProcessPolicy([]string{dir}, nil); err != nil {
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
