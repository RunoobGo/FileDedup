package main

// APP-1（2026-09-21 全量审查，设计稿 §15.0-A）：AS-R3 只修了一半。
//
// 同一件事在两个策略入口上形状完全一样——持 a.mu 的窗口里经 ops 的策略函数
// 走到 fscase.Sensitive，而它**要往用户目录写探测文件**再反向 Lstat。
// 处理策略侧（PreviewProcessPolicy）已经在 AS-R3 里补了 WarmSensitivity +
// ApplyProcessPolicyWith；保留策略侧（ApplyKeepPolicy → ops.ApplyKeepPolicy →
// pickInDirectory）至今就地实测，于是：
//
//   - 死挂载（网络盘拔走后 stat 挂死）时，一次"应用保留策略"点下去就能把 a.mu
//     连同全部 Wails 绑定一起卡住；
//   - 而 ApplyKeepPolicy 的注释写的是"遍历阶段全程持锁"，读起来像纯内存计算。
//
// 观测点与 AS-R3 同一个：fscase.SetProbeHook 在探测真正动手前回调，
// 回调时 a.mu 是否被持有，就是"这次 I/O 落在锁内还是锁外"的直接证据。

import (
	"path/filepath"
	"testing"

	"filededup/internal/fscase"
	"filededup/internal/model"
)

// observeKeepPolicyProbes 应用一次目录保留策略，返回目标目录的探测次数，
// 以及"探测发生那一刻 a.mu 是否被持有"。
func observeKeepPolicyProbes(t *testing.T, a *App, dir string) (probes int, underLock bool) {
	t.Helper()
	apply := func() {
		if _, err := a.ApplyKeepPolicy(model.KeepPolicy{
			Kind: "directory", Directories: []string{dir},
		}); err != nil {
			t.Fatal(err)
		}
	}
	fscase.SetProbeHook(func(d string) {
		if filepath.Clean(d) != filepath.Clean(dir) {
			return
		}
		probes++
		if !a.mu.TryLock() {
			underLock = true
			return
		}
		a.mu.Unlock()
	})
	apply()
	fscase.SetProbeHook(nil)
	return probes, underLock
}

// TestApplyKeepPolicyDoesNotProbeUnderLock 锁内不得有卷语义探测（AS-R3 同一条纪律）。
func TestApplyKeepPolicyDoesNotProbeUnderLock(t *testing.T) {
	a, _, _, _, _ := procFixture(t)
	// 必须用一个**没被探测过**的目录：结论按目录缓存，命中缓存就不写盘，
	// 那样本用例什么都没观测到却会绿——所以下面把"确实触发了探测"钉成前置条件。
	probes, underLock := observeKeepPolicyProbes(t, a, t.TempDir())
	if probes == 0 {
		t.Fatal("没有触发任何卷语义探测，本用例什么都没观测到（夹具失效，不是通过）")
	}
	if underLock {
		t.Fatal("在持有 a.mu 期间做了写盘探测：死挂载会卡住全部应用绑定（APP-1）。" +
			"敏感性必须在取锁之前预热好，锁内只做纯比较。")
	}
}

// TestApplyKeepPolicyProbeIsCacheHitAfterWarm 预热只付一次写盘代价。
//
// 这条守住修向的前提：若预热没生效（With 变体拿到 nil 解析器、或预热的目录与
// 实际询问的目录不是同一个键），每次应用策略都会往用户目录写文件。
func TestApplyKeepPolicyProbeIsCacheHitAfterWarm(t *testing.T) {
	a, _, _, _, _ := procFixture(t)
	dir := t.TempDir()
	if got, _ := observeKeepPolicyProbes(t, a, dir); got != 1 {
		t.Fatalf("首次应用保留策略触发 %d 次探测，应为 1 次（结论按目录缓存，多探说明每次都重算）", got)
	}
	if got, _ := observeKeepPolicyProbes(t, a, dir); got != 0 {
		t.Fatalf("第二次应用同一策略触发 %d 次探测，应为 0 次（该目录已预热）", got)
	}
}
