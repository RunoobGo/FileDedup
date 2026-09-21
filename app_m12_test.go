package main

// M12b 应用侧探针（2026-09-21 全仓审计 §五 12）：账本影像损坏被隔离重建后，
// 原因必须能被界面取到。走"前端初始拉取"而不是 emit，是因为 startup 跑在
// 前端注册监听器之前，那会儿发出去的事件必丢（见 App.startupNotice 注释）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/model"
)

func TestOpenLedgerSurfacesQuarantine(t *testing.T) {
	a := newTestApp(t)
	rec := &eventRecorder{done: make(chan string, 8)}
	a.emit = rec.emit
	junk := []byte("garbage where an undo ledger used to live")
	p := filepath.Join(a.cfgDir, "history.db")
	if err := os.WriteFile(p, junk, 0o644); err != nil {
		t.Fatal(err)
	}

	a.openLedger()

	a.mu.Lock()
	hs := a.hist
	a.mu.Unlock()
	if hs == nil {
		t.Fatal("确证损坏应隔离重建并留下可用句柄（否则清理会被账本不可用挡死）")
	}
	defer hs.Close()

	notice := a.GetStartupNotice()
	if notice == "" {
		t.Fatal("隔离重建后界面取到空提示：用户只会看到「历史记录是空的」，不知道发生过什么")
	}
	// 文案必须回答三个问题：为什么空了、旧文件还在不在、后果是什么。
	// 少任何一条，用户就无法判断"能不能自己救回来"。
	for _, want := range []string{"影像损坏", "旧文件仍在", "无法再回撤"} {
		if !strings.Contains(notice, want) {
			t.Fatalf("提示缺少「%s」这一层信息：%q", want, notice)
		}
	}
	matches, _ := filepath.Glob(p + ".broken-*")
	if len(matches) != 1 {
		t.Fatalf("隔离文件 = %v, want 1 个", matches)
	}
	if base := filepath.Base(matches[0]); !strings.Contains(notice, base) {
		t.Fatalf("提示应点名隔离后的文件（用户要靠它找回旧账本），实得 %q / 文件名 %s", notice, base)
	}
	// 隔离的旧影像必须还挂在原目录（文案承诺"仍在配置目录"）
	kept, err := os.ReadFile(matches[0])
	if err != nil || string(kept) != string(junk) {
		t.Fatalf("隔离文件不可读或内容不符: %v %q", err, kept)
	}
}

// 反面：健康的账本库不得凭空报"丢了"——那是一条假警，
// 而且每次启动都弹一次的话，用户会对这类提示脱敏。
func TestOpenLedgerQuietOnHealthyLedger(t *testing.T) {
	a := newTestApp(t)
	a.openLedger() // 库不存在 → 正常新建
	if got := a.GetStartupNotice(); got != "" {
		t.Fatalf("新建库不该有启动提示：%q", got)
	}
	a.mu.Lock()
	hs := a.hist
	a.mu.Unlock()
	if hs == nil {
		t.Fatal("openLedger 应留下可用句柄")
	}
	if _, err := hs.SaveScan(model.ScanConfig{Roots: []string{"/x"}},
		[]*model.DuplicateGroup{mkGroup(1, 100, "/x/a.bin", "/x/b.bin")}, nil); err != nil {
		t.Fatalf("新建库不可写: %v", err)
	}
	// 关掉句柄，模拟"下次启动重开同一个健康库"
	hs.Close()
	a.mu.Lock()
	a.hist = nil
	a.mu.Unlock()

	a.openLedger() // 第二次打开已有健康库
	if got := a.GetStartupNotice(); got != "" {
		t.Fatalf("健康库重开不该有提示：%q", got)
	}
	a.mu.Lock()
	hs2 := a.hist
	a.mu.Unlock()
	if hs2 == nil {
		t.Fatal("重开健康库应留下可用句柄")
	}
	hs2.Close()
}
