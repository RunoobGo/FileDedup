package main

// M7（2026-09-21 全仓审计 §五 7）：账本写失败必须**让用户看见**。
//
// 现场：打包后的 GUI 没有控制台。修正前 FinishItem / FinalizeOp / persistKeepPaths /
// MarkItemUndo 四处落账失败只 `fmt.Fprintf(os.Stderr, ...)`，于是磁盘满时条目停在
// planned、`ops:done` 横幅照报「成功」，用户按 09 §6.7 以为可撤——实际账本上
// 什么都没有，永久失去撤销入口。
//
// 本文件钉住统一出口 warnLedger（stderr + app:error 双通道）在各落账点都生效，
// 并钉住两件容易做错的事：
//   ① 留痕不得遮蔽真实错误（回撤的返回值仍是回撤本身的失败原因）；
//   ② 留痕不得改变执行结论（账本坏了也不要把已完成的清理报成失败）。

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filededup/internal/history"
	"filededup/internal/model"
	"filededup/internal/ops"
)

// linkedHistApp 备好「结果集 + 当前历史行」的 App，不碰真实文件。
//
// 历史行必须存在：persistKeepPaths 与 ExecuteOperation 的 histID 都取自 curHistID，
// 为 0 时两处都会直接跳过，断言就成了空转。磁盘上不存在的路径按 S8 判 Skipped，
// 照样走完逐条收口与 Finalize（见 TestOpJournalTrashSkippedItems）。
func linkedHistApp(t *testing.T) (*App, *eventRecorder, int64) {
	t.Helper()
	a, rec := newHistApp(t)
	g := mkHistGroup(1, 1000, "/m7/a/x.bin", "/m7/b/y.bin", "/m7/c/z.bin")
	histID, err := a.hist.SaveScan(model.ScanConfig{Roots: []string{"/m7"}},
		[]*model.DuplicateGroup{g}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.LoadScanHistory(histID); err != nil {
		t.Fatal(err)
	}
	if a.curHistID != histID {
		t.Fatalf("curHistID = %d, want %d", a.curHistID, histID)
	}
	return a, rec, histID
}

// countEvent 数某事件出现了几次。只看"有没有 app:error"是不够的：
// 只给收尾留痕、把逐条收口的失败藏进最后一条，同样能骗过布尔断言。
func countEvent(rec *eventRecorder, name string) int {
	n := 0
	for _, e := range rec.names() {
		if e == name {
			n++
		}
	}
	return n
}

// ledgerErrText 取出最近一条 app:error 的文案。
func ledgerErrText(t *testing.T, rec *eventRecorder) string {
	t.Helper()
	p, ok := rec.lastWith("app:error")
	if !ok {
		t.Fatal("没有任何 app:error：账本写失败仍然是静默的")
	}
	m, ok := p.(map[string]string)
	if !ok {
		t.Fatalf("app:error 载荷类型 %T，与 app.go 的 map[string]string 口径不符", p)
	}
	return m["error"]
}

// closeLedger 让账本从此刻起所有写入报错（模拟磁盘满 / 句柄失效）。
func closeLedger(t *testing.T, a *App) {
	t.Helper()
	if err := a.hist.Close(); err != nil {
		t.Errorf("关闭账本失败: %v", err)
	}
}

// TestPersistKeepPathsFailureIsVisible 保留决策落不进账本时必须显式告警。
// 这一条最阴的地方是"看起来成功了"：界面已按新策略标好保留项，
// 下次从历史页恢复时保留项静默回到上一次保存的状态。
func TestPersistKeepPathsFailureIsVisible(t *testing.T) {
	a, rec, _ := linkedHistApp(t)
	closeLedger(t, a)

	if err := a.ClearKeepDecisions(); err != nil {
		t.Fatalf("留痕不得改变执行结论: %v", err)
	}
	if !rec.has("app:error") {
		t.Fatalf("保留决策落账失败被静默吞掉（事件序列 %v）", rec.names())
	}
	if txt := ledgerErrText(t, rec); !strings.Contains(txt, "保留决策") {
		t.Fatalf("告警文案未说明是哪一笔: %q", txt)
	}
}

// TestExecuteOperationLedgerFailureIsVisible 操作执行到一半账本变得不可写：
// 逐条收口（FinishItem）与总收尾（FinalizeOp）两处都不得静默。
//
// 用 opsExecuteFn 接缝在真实执行**之前**关库，于是每次 FinishItem 与随后的
// FinalizeOp 必然报错——这是确定性场景，不是撞竞态。
func TestExecuteOperationLedgerFailureIsVisible(t *testing.T) {
	a, rec, _ := linkedHistApp(t)
	prev := opsExecuteFn
	opsExecuteFn = func(o ops.Options, op model.OpRequest) model.OpsResult {
		closeLedger(t, a) // 关库发生在 goroutine 里，只用 t.Errorf
		return prev(o, op)
	}
	t.Cleanup(func() { opsExecuteFn = prev })

	got, restoreErrs := captureAppErrors(a) // 全部 app:error，不看末条（见下方更正）
	t.Cleanup(restoreErrs)

	a.mu.Lock()
	var sel []uint64
	for _, f := range a.groups[0].Files {
		sel = append(sel, f.ID)
	}
	a.mu.Unlock()
	if _, err := a.ExecuteOperation(model.OpRequest{Kind: "trash", FileIDs: sel}); err != nil {
		t.Fatal(err)
	}
	waitOpsDone(t, rec)

	if n := countEvent(rec, "app:error"); n < 2 {
		t.Fatalf("app:error 只有 %d 条：条目收口与账本收尾必须各自留痕（只报收尾等于把逐条失败藏起来）", n)
	}
	// ★ 2026-09-21 APP-3 就地更正（设计稿 §15.0-A）：这里原先取 rec 的**末条**
	//   app:error 并断言它含「收尾」。那不是钉错了语义，而是钉错了**载体**——
	//   eventRecorder.payloads 按事件名只留最后一条载荷，于是"FinalizeOp 失败必须
	//   可见"这一条性质被实现成"收尾那条必须是最后一条"。APP-3 把 ExecuteOperation
	//   收尾之后的 PruneScanFiles 失败也接进统一出口（它此前只写 stderr），末条
	//   因此合法地换成了裁剪告警，原断言就把"多留了一处痕"读成"少了收尾那条"。
	//   现在按集合断言：收尾那条必须在场、必须说到"收尾"与"计划中"；顺序不钉。
	var finalizeTxt string
	for _, txt := range *got {
		if strings.Contains(txt, "收尾") {
			finalizeTxt = txt
			break
		}
	}
	if finalizeTxt == "" {
		t.Fatalf("没有任何一条告警指向 FinalizeOp（账本收尾失败被吞掉）: %q", strings.Join(*got, " | "))
	}
	if !strings.Contains(finalizeTxt, "计划中") {
		t.Fatalf("收尾失败必须说明后果（残留 planned 影响回撤范围）: %q", finalizeTxt)
	}
}

// TestUndoExecuteItemLedgerFailureKeepsRealError 回撤后态落库失败：
// 既要留痕，也不得把回撤本身的失败原因换成"落库出错"。
func TestUndoExecuteItemLedgerFailureKeepsRealError(t *testing.T) {
	dir := t.TempDir()
	content := []byte("M7-UNDO-LEDGER-AFTER-ACTION")
	dest := filepath.Join(dir, "trash", "a.bin")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, content, 0o644); err != nil {
		t.Fatal(err)
	}
	orig := filepath.Join(dir, "home", "a.bin")
	a, rec := newHistApp(t)
	opID := seedExecutedOp(t, a, "trash", []string{orig}, []string{dest},
		uint64(len(content)), [][32]byte{blake3Of(t, dest)}, time.Now().UnixNano())
	_, items, err := a.hist.GetOp(opID)
	if err != nil || len(items) != 1 {
		t.Fatalf("账本条目未备好: n=%d err=%v", len(items), err)
	}

	prev := undoOneFn
	t.Cleanup(func() { undoOneFn = prev })
	wantErr := errors.New("模拟：回撤动作自身失败")
	undoOneFn = func(ops.UndoItem) (string, error) {
		closeLedger(t, a) // 写前落账已成，动作之后账本不可写 → 失败态落库必错
		return "", wantErr
	}

	_, gotErr := a.undoExecuteItem(a.hist, "trash", history.OpItem{
		ID: items[0].ID, OrigPath: orig, DestPath: dest,
		Size: uint64(len(content)),
	})
	// ① 真实原因优先：调用方按返回值汇总成败，换成"落库出错"就报错了事实。
	if !errors.Is(gotErr, wantErr) {
		t.Fatalf("回撤返回值 = %v, want %v（落库留痕不得遮蔽真实错误）", gotErr, wantErr)
	}
	// ② 失败态没落库也必须可见：否则该条永远停在 undoing，界面读作"回撤进行中"。
	if !rec.has("app:error") {
		t.Fatalf("失败态落库出错被静默吞掉（事件序列 %v）", rec.names())
	}
	if txt := ledgerErrText(t, rec); !strings.Contains(txt, "回撤中") {
		t.Fatalf("告警未说明后果（条目停在 undoing）: %q", txt)
	}
}
