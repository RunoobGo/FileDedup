package scanner

// M67（04 §6.11 SCN-5；设计段 §19.0-3 / §19.1-5）的"修前必红"探针。
//
// Visited 登记的口径写的是"实际访问目录数"，实现却是 len(visited) —— 而 visited
// 那张表同时兼任**去重集**（子目录在 submit 时就登记，不等真的读过），于是三处
// 都不是"访问过"：
//   ① 全部根在开池前无条件预置（scanner.go:247-249），根 ReadDir 失败也算进去了；
//   ② 取消后排空的路径一次 I/O 都不做，但根那条登记还在；
//   ③ 子目录在入队时就登记（:368-377），队列里没来得及处理的也算。
//
// 因为 visited 必须保留去重职责（否则同一棵树会被重复入队），修法只能是"拆成两个
// 东西"：visited 继续做去重集，另计一个"ReadDir 成功后 ++"的 accessed。
// 本文件的判据就是这条新口径，三条各自钉一格。
//
// 全部三格只引用改前就有的符号（WalkWithGate / Result.Visited / testGate），
// 所以三条都必须在改前树真跑并各自红一次。

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"filededup/internal/model"
)

// TestVisitedCountsOnlySuccessfulReadDir P-19-6。
//
// 两格：不存在的根（ReadDir 必失败，与 euid 无关，比 chmod 0 更确定）⇒ 一格都不许计；
// 正常全遍历 ⇒ 必须与实际读过的目录数**相等**（这条是防"为了变绿把计数改成 0"，
// 也防收口时把 accessed 漏加）。
func TestVisitedCountsOnlySuccessfulReadDir(t *testing.T) {
	// ① 根读不了：诊断计数不得虚报"访问过"。
	gone := filepath.Join(t.TempDir(), "never-existed")
	res := WalkWithGate(context.Background(), []string{gone}, &model.Filters{}, 2, nil)
	if len(res.Failed) == 0 {
		t.Fatalf("前提自检：不存在的根应当产生一条 Failed，实测 %+v", res.Failed)
	}
	if res.Visited != 0 {
		t.Errorf("根 ReadDir 失败时 Visited 必须为 0，实测 %d：改前形态把预置进 visited 的根算成\"访问过\"，"+
			"而这个目录一次读都没有成功", res.Visited)
	}

	// ② 正常全遍历：1 个根 + 4 个子目录 = 5 次成功的 ReadDir。
	root := makeDeepTree(t, 4, 3)
	res2 := WalkWithGate(context.Background(), []string{root}, &model.Filters{}, 4, nil)
	if len(res2.Files) != 12 {
		t.Fatalf("前提自检：夹具应产出 12 个文件，实测 %d", len(res2.Files))
	}
	if res2.Visited != 5 {
		t.Errorf("正常全遍历的 Visited 必须等于真读过的目录数 5，实测 %d", res2.Visited)
	}
}

// TestWalkCancelDrainReadsNoDirectories P-19-5。
//
// 与 TestWalkWithGateCancelDrainsWithoutIO 同一夹具与同一时序（先暂停保证取消发生在
// 任何目录被处理之前），但断言的是**正证**：一次 ReadDir 都没有 ⇒ Visited 必须为 0。
// 原钉子只能证明"没收齐"，而"没收齐"在取消发生在中途时同样成立，钉不住零 I/O。
func TestWalkCancelDrainReadsNoDirectories(t *testing.T) {
	root := makeDeepTree(t, 60, 2)
	ctx, cancel := context.WithCancel(context.Background())
	gate := &testGate{}
	gate.Pause()

	done := make(chan *Result, 1)
	go func() {
		done <- WalkWithGate(ctx, []string{root}, &model.Filters{}, 4, gate)
	}()
	// 前提自检：暂停发生在 WalkWithGate 启动**之前**，于是第一个 worker 必然停在
	// gate.Wait 上、一次 ReadDir 都没做过。这条 select 就是在验这个前提：
	// 如果它在取消之前就把结果交回来了，本用例测的就不是"取消排空"那一档。
	select {
	case res := <-done:
		t.Fatalf("前提自检失败：暂停期间 Walk 就返回了（files=%d visited=%d），没测到取消那一档",
			len(res.Files), res.Visited)
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	gate.Resume()

	var res *Result
	select {
	case res = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("取消后 Walk 未在 5s 内返回")
	}
	if res.Visited != 0 {
		t.Errorf("取消排空后 Visited 必须为 0（零磁盘 I/O 的正证），实测 %d、files=%d", res.Visited, len(res.Files))
	}
	if len(res.Files) != 0 {
		t.Errorf("取消发生在任何目录处理之前，一个文件都不该收进来，实测 %d", len(res.Files))
	}
}
