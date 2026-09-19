package main

// v0.5.0 功能 3（Task 5）：扫描历史自动保存 / 恢复 / 门槛改造 / 清理联动裁剪。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/history"
	"filededup/internal/model"
)

// newHistApp 带真实历史库与事件记录桩的 App（不跑扫描时 hist 为空库）。
func newHistApp(t *testing.T) (*App, *eventRecorder) {
	t.Helper()
	a := NewApp()
	a.ctx = context.Background() // emit 已替换，不再要求 Wails 内部 context
	a.cfgDir = t.TempDir()
	rec := &eventRecorder{done: make(chan string, 8)}
	a.emit = rec.emit
	hs, err := history.Open(filepath.Join(a.cfgDir, "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	a.hist = hs
	t.Cleanup(func() { _ = hs.Close() })
	return a, rec
}

// mkHistGroup 磁盘上不存在的组（历史恢复测试专用：操作时 ENOENT → S8 Skipped，
// 无需真实文件即可走完 ExecuteOperation 收尾 + 历史裁剪路径）。
func mkHistGroup(id uint64, size uint64, paths ...string) *model.DuplicateGroup {
	return mkGroup(id, size, paths...)
}

// 扫描成功收尾必须自动写历史（scan:done 事件发出前已落库）。
func TestScanDoneSavesHistory(t *testing.T) {
	root := t.TempDir()
	payload := []byte("HISTORY-PAYLOAD-DUPLICATE")
	if err := os.WriteFile(filepath.Join(root, "a.bin"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "b.bin"), payload, 0o644); err != nil {
		t.Fatal(err)
	}

	a, rec := newHistApp(t)
	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}, Threads: 3}); err != nil {
		t.Fatal(err)
	}
	if ev := rec.waitTerminal(t, "scan"); ev != "scan:done" {
		t.Fatalf("终止事件 = %s", ev)
	}
	ms, err := a.ListScanHistory()
	if err != nil || len(ms) != 1 {
		t.Fatalf("scan:done 后应有 1 条历史: %+v err=%v", ms, err)
	}
	m := ms[0]
	if m.Groups < 1 || m.Files < 2 || m.Files != m.OrigFiles || m.Reclaimable == 0 {
		t.Fatalf("历史计数失真: %+v", m)
	}
	if len(m.Roots) != 1 || m.Roots[0] != root || m.Threads != 3 {
		t.Fatalf("历史配置失真: %+v", m)
	}
	a.mu.Lock()
	ready, cur := a.resultsReady, a.curHistID
	a.mu.Unlock()
	if !ready || cur != m.ID {
		t.Fatalf("当前历史联动未建立: ready=%v cur=%d", ready, cur)
	}
}

// 历史恢复 → Idle 引擎下可继续清理 → 结果集与历史同步裁剪；
// 同时覆盖恢复正确性（组/文件/ID/isKeep 建议）。
func TestLoadHistoryAndExecutePrunes(t *testing.T) {
	a, rec := newHistApp(t)
	g1 := mkHistGroup(1, 1000, "/hist/a/x.bin", "/hist/b/y.bin")
	g2 := mkHistGroup(2, 2000, "/hist/c/x.bin", "/hist/d/y.bin", "/hist/e/z.bin")
	groups := []*model.DuplicateGroup{g1, g2}
	id, err := a.hist.SaveScan(model.ScanConfig{Roots: []string{"/hist"}}, groups, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.hist.UpdateKeepPaths(id, []string{"/hist/a/x.bin"}); err != nil {
		t.Fatal(err)
	}

	summ, err := a.LoadScanHistory(id)
	if err != nil {
		t.Fatalf("载入历史失败: %v", err)
	}
	if summ.Groups != 2 || summ.Reclaimable != 5000 {
		t.Fatalf("摘要失真: %+v", summ)
	}
	if s := a.pipe.Status(); s != model.StatusIdle {
		t.Fatalf("前提应为从未运行扫描: %s", s)
	}
	r, _ := a.GetResultGroups(ResultQuery{})
	if r.Total != 2 {
		t.Fatalf("恢复组数 = %d, want 2", r.Total)
	}
	seen := map[uint64]bool{}
	keepOK := false
	for _, g := range r.Groups {
		for _, f := range g.Files {
			if seen[f.ID] {
				t.Fatalf("恢复 ID 冲突: %d", f.ID)
			}
			seen[f.ID] = true
			if f.Path == "/hist/a/x.bin" && f.IsKeep {
				keepOK = true
			}
		}
	}
	if len(seen) != 5 || !keepOK {
		t.Fatalf("恢复文件/保留建议失真: n=%d keep=%v", len(seen), keepOK)
	}

	// 清理 g1 的冗余项（磁盘不存在 → S8 Skipped），验证操作被受理
	a.mu.Lock()
	var sel []uint64
	for _, f := range a.groups[0].Files {
		if f.Path != "/hist/a/x.bin" {
			sel = append(sel, f.ID)
		}
	}
	a.mu.Unlock()
	if _, err := a.ExecuteOperation(model.OpRequest{Kind: "trash", FileIDs: sel}); err != nil {
		t.Fatalf("历史恢复后 Idle 状态应可执行清理: %v", err)
	}
	if ev := rec.waitTerminal(t, "ops"); ev != "ops:done" {
		t.Fatalf("ops 终止事件 = %s", ev)
	}

	r2, _ := a.GetResultGroups(ResultQuery{})
	if r2.Total != 1 { // g1 只剩保留项 → 整组移出
		t.Fatalf("结果集未裁剪: total=%d", r2.Total)
	}
	ms, err := a.ListScanHistory()
	if err != nil || len(ms) != 1 {
		t.Fatal(err)
	}
	if ms[0].Groups != 1 || ms[0].Files != 3 || ms[0].OrigFiles != 5 || ms[0].Reclaimable != 4000 {
		t.Fatalf("历史未同步裁剪: %+v", ms[0])
	}
}

// 门槛改造：未扫描/未恢复时拒绝操作；历史库缺失时四绑定明确报错。
func TestHistoryGatesAndUnavailable(t *testing.T) {
	a := newTestApp(t)
	a.groups = []*model.DuplicateGroup{mkGroup(1, 100, "a", "b")}
	a.byID[101] = a.groups[0].Files[0]
	if _, err := a.ExecuteOperation(model.OpRequest{Kind: "trash", FileIDs: []uint64{101}}); err == nil ||
		!strings.Contains(err.Error(), "暂无可操作的结果集") {
		t.Fatalf("resultsReady=false 应拒绝操作: %v", err)
	}
	if _, err := a.ListScanHistory(); err == nil || !strings.Contains(err.Error(), "历史库不可用") {
		t.Fatalf("hist=nil 应报历史库不可用: %v", err)
	}
	if _, err := a.LoadScanHistory(1); err == nil || !strings.Contains(err.Error(), "历史库不可用") {
		t.Fatalf("hist=nil 应报历史库不可用: %v", err)
	}
	if err := a.DeleteScanHistory(1); err == nil || !strings.Contains(err.Error(), "历史库不可用") {
		t.Fatal("hist=nil 应报历史库不可用")
	}
	if err := a.ClearScanHistory(); err == nil || !strings.Contains(err.Error(), "历史库不可用") {
		t.Fatal("hist=nil 应报历史库不可用")
	}
}

// 保留决策必须随当前历史行持久化（恢复后建议不丢）。
func TestKeepPolicyPersistsKeepPaths(t *testing.T) {
	a, _ := newHistApp(t)
	g := mkHistGroup(1, 100, "/kp/a/old.bin", "/kp/b/new.bin")
	g.Files[0].ModTime = 1000
	g.Files[1].ModTime = 9000
	id, err := a.hist.SaveScan(model.ScanConfig{Roots: []string{"/kp"}},
		[]*model.DuplicateGroup{g}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.LoadScanHistory(id); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ApplyKeepPolicy(model.KeepPolicy{Kind: "newest"}); err != nil {
		t.Fatal(err)
	}
	meta, _, err := a.hist.LoadScan(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.KeepPaths) != 1 || meta.KeepPaths[0] != "/kp/b/new.bin" {
		t.Fatalf("保留决策未落库: %+v", meta.KeepPaths)
	}

	a.ClearKeepDecisions()
	meta, _, _ = a.hist.LoadScan(id)
	if len(meta.KeepPaths) != 0 {
		t.Fatalf("重置未同步落库: %+v", meta.KeepPaths)
	}
}
