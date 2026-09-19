package main

// 2026-09-18 审查第 3 批 B3-1：账本/保留决策的在途互斥。
//
// 清理与回撤在途时，账本正被 FinishItem/FinalizeOp 逐项落账、结果集正按派发时
// 的 keep 快照执行。此时整表删除账本或改写保护集，会让界面事实与文件系统事实
// 分叉（前者显示"清空/重置成功"，后者仍在按旧状态动文件），因此两者都必须拒绝。

import (
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/history"
)

// appWithLedger 带历史库与一条已落账记录的 App。
func appWithLedger(t *testing.T) (*App, int64) {
	t.Helper()
	a := newTestApp(t)
	hs, err := history.Open(filepath.Join(a.cfgDir, "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hs.Close() })
	a.hist = hs
	opID, err := hs.BeginOp("trash", "", 0, true, []history.OpItemPlan{
		{OrigPath: "/ledger/a.bin", Size: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := hs.FinishItem(opID, "/ledger/a.bin", "/ledger/.trash/a.bin", "", history.StateDone, ""); err != nil {
		t.Fatal(err)
	}
	if err := hs.FinalizeOp(opID); err != nil {
		t.Fatal(err)
	}
	return a, opID
}

func setOpsRunning(a *App, v bool) {
	a.mu.Lock()
	a.opsRunning = v
	a.mu.Unlock()
}

// 清理/回撤在途时清空记录必须被拒，且账本内容一处不少。
func TestClearOpRecordsRejectedWhileOpsRunning(t *testing.T) {
	a, opID := appWithLedger(t)
	setOpsRunning(a, true)

	err := a.ClearOpRecords()
	if err == nil {
		t.Fatal("在途清空记录应被拒绝（账本正在逐项落账）")
	}
	if !strings.Contains(err.Error(), "执行中") {
		t.Fatalf("错误应说明在途原因: %v", err)
	}
	meta, items, err := a.hist.GetOp(opID)
	if err != nil || meta == nil || len(items) != 1 {
		t.Fatalf("账本记录必须仍在: meta=%v items=%d err=%v", meta, len(items), err)
	}

	// 空闲后照常可清空
	setOpsRunning(a, false)
	if err := a.ClearOpRecords(); err != nil {
		t.Fatal(err)
	}
	if metas, _ := a.hist.ListOps(); len(metas) != 0 {
		t.Fatalf("空闲时应清空，剩 %d 条", len(metas))
	}
}

// 重置保留策略与 ApplyKeepPolicy 同口径：在途拒绝且不改写现有决策。
func TestClearKeepDecisionsRejectedWhileOpsRunning(t *testing.T) {
	a := newTestApp(t)
	a.keepIDs = map[uint64]bool{7: true}
	setOpsRunning(a, true)

	err := a.ClearKeepDecisions()
	if err == nil {
		t.Fatal("在途重置保留策略应被拒绝")
	}
	if !strings.Contains(err.Error(), "执行中") {
		t.Fatalf("错误应说明在途原因: %v", err)
	}
	if !a.keepIDs[7] {
		t.Fatal("拒绝时保护集不得改变")
	}

	setOpsRunning(a, false)
	if err := a.ClearKeepDecisions(); err != nil {
		t.Fatal(err)
	}
	if len(a.keepIDs) != 0 {
		t.Fatalf("空闲时应清空保留决策: %v", a.keepIDs)
	}
}
