package main

// M322（APP-36，2026-09-28 第六轮全量审查）：`GetFailedItems` 在锁内 return 内部切片，
// 序列化发生在锁外 ⇒ 交出去的那段数组与 `a.failed` 同底层。
//
// ★ 判据落点与设计稿 §8.1 的说法不同（实施批开码复核后的更正）：设计稿写的是
// "扫描腿在 a.mu 下 append 到 a.failed，命中预留容量时原位写同一数组" ——现读
// app.go 五处写点（:755 nil、:807 整表、:2241 整表、:2630 先 copy 再 append、
// :2540 只是取头）全是**整表替换**，没有一个原位写者，那条竞态今天构造不出来。
// 但同一条根因（交内部切片的头）有一格是**当场可红**的：调用方一笔写直接改到
// App 状态。副本把它和将来可能出现的原位写一起切断。
// 变异 M322-a：去掉 copy 改回 `return a.failed` ⇒ 第一条断言红。

import (
	"testing"

	"filededup/internal/model"
)

func TestM322GetFailedItemsHandsOutACopyNotTheInternalSlice(t *testing.T) {
	a := NewApp()
	a.mu.Lock()
	a.failed = []model.FailedItem{{Path: "/x.bin", Stage: "scan", Err: "读不动"}}
	a.mu.Unlock()

	snap := a.GetFailedItems()
	if len(snap) != 1 {
		t.Fatalf("前提：清单里应有一条，实得 %d", len(snap))
	}
	// 绑定调用方只是拿着返回值序列化，中途改一个字段都不该影响 App。
	snap[0].Path = "/tampered-by-caller"
	snap[0].Err = "被快照写穿了"

	if again := a.GetFailedItems(); again[0].Path != "/x.bin" || again[0].Err != "读不动" {
		t.Fatalf("调用方写快照写进了内部清单（交的是 a.failed 的头，别名没切断）: %+v", again[0])
	}

	// 空集那一臂维持前端契约：`[]` 而不是 `null`（副本改动不能把它带回 nil）。
	a.mu.Lock()
	a.failed = nil
	a.mu.Unlock()
	if got := a.GetFailedItems(); got == nil || len(got) != 0 {
		t.Errorf("空失败清单应交出非 nil 空切片（前端契约：[] 不是 null），实得 %#v", got)
	}
}
