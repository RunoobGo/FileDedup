package main

// 2026-09-18 审查 C7 回归：结果集代际号。
//
// 被守住的时序：扫描 goroutine 在 scanInFlight 复位后还有「SaveScan（锁外）→
// resultsReady/curHistID → scan:done」三拍。这期间用户完全可以重扫（互斥标志
// 已经放下），旧收尾会把新任务的 curHistID 换成自己的历史行 —— 之后每次清理
// 裁剪的都是上一条记录，当前记录永远残留已删文件。

import (
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/model"
)

func TestStartScanAdvancesGeneration(t *testing.T) {
	root := t.TempDir()
	payload := []byte("GENERATION-PAYLOAD")
	for _, p := range []string{"a.bin", "sub/b.bin"} {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, payload, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	a, rec := newHistApp(t)
	before := a.resultGen.Load()
	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}, Threads: 2}); err != nil {
		t.Fatal(err)
	}
	if ev := rec.waitTerminal(t, "scan"); ev != "scan:done" {
		t.Fatalf("终止事件 = %s", ev)
	}
	if got := a.resultGen.Load(); got != before+1 {
		t.Fatalf("代际号 = %d, want %d（新扫描必须换代）", got, before+1)
	}
	// 当代任务必须认领成功：curHistID 指向本次扫描落库的那一行
	ms, err := a.ListScanHistory()
	if err != nil || len(ms) != 1 {
		t.Fatalf("历史 = %+v err=%v", ms, err)
	}
	a.mu.Lock()
	ready, cur := a.resultsReady, a.curHistID
	a.mu.Unlock()
	if !ready || cur != ms[0].ID {
		t.Fatalf("当代扫描认领失败: ready=%v cur=%d want %d", ready, cur, ms[0].ID)
	}

	// 重扫一次：旧任务的代际此刻必须被判为「已被取代」
	a.mu.Lock()
	stale := a.resultGen.Load()
	a.mu.Unlock()
	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}, Threads: 2}); err != nil {
		t.Fatal(err)
	}
	if ev := rec.waitTerminal(t, "scan"); ev != "scan:done" {
		t.Fatalf("第二次扫描终止事件 = %s", ev)
	}
	if !a.resultSuperseded(stale) {
		t.Fatal("重扫后旧代际必须判为已被取代")
	}
}

// 判定本身：只有代际相等才允许认领。
func TestResultSupersededPredicate(t *testing.T) {
	a, _ := newHistApp(t)
	a.mu.Lock()
	myGen := a.resultGen.Add(1)
	if a.resultSuperseded(myGen) {
		t.Fatal("刚领到的代际不应判为过期")
	}
	a.resultGen.Add(1) // 模拟新扫描/新载入接管
	a.mu.Unlock()
	if !a.resultSuperseded(myGen) {
		t.Fatal("被接管后必须判为过期（否则旧收尾会覆盖 curHistID）")
	}
}

// 历史载入同样是一次接管：否则载入后旧扫描收尾仍会把 curHistID 写回自己那行。
func TestLoadScanHistoryAdvancesGeneration(t *testing.T) {
	a, _ := newHistApp(t)
	id, err := a.hist.SaveScan(model.ScanConfig{Roots: []string{"/u"}},
		[]*model.DuplicateGroup{mkHistGroup(1, 1000, "/u/a/x.bin", "/u/b/y.bin")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	before := a.resultGen.Load()
	if _, err := a.LoadScanHistory(id); err != nil {
		t.Fatal(err)
	}
	if a.resultGen.Load() == before {
		t.Fatal("载入历史未换代替际号，旧扫描收尾仍可覆盖结果集归属")
	}
}
