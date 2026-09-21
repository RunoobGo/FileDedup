package main

// M25 应用侧探针（2026-09-21，04 §6.8.8 M25）：哈希缓存打开失败必须走界面通道。
//
// 为什么必须走界面而不是 stderr：GUI 没有终端。失败的实际后果不是崩溃而是
// **永久变慢且无从排查**——缓存没开成，每次扫描全量重算，用户只感到"这软件越用越慢"。
// 探针夹具与 E11 的真读数同源：把 <cfgDir>/cache.db 建成**目录**，cache.Open 必失败
// 且**不走隔离重建**（往里面写垃圾反而会命中"影像损坏→隔离→重建成功"，拿不到失败态）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// breakCache 造出"缓存打不开"的现场：cache.db 是个目录。
func breakCache(t *testing.T, cfgDir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(cfgDir, "cache.db"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestOpenCacheUnavailableSurfacesNotice(t *testing.T) {
	a := newTestApp(t)
	breakCache(t, a.cfgDir)

	a.openCache()

	a.mu.Lock()
	cch := a.cch
	a.mu.Unlock()
	if cch != nil {
		t.Fatal("夹具失效：cache.db 建成目录后仍拿到了句柄，本用例没在测失败路径")
	}
	notice := a.GetStartupNotice()
	if notice == "" {
		t.Fatal("缓存打开失败只写 stderr：GUI 无终端 ⇒ 用户看到的是「这软件越来越慢」，无法自查")
	}
	// 三件事缺一不可：出了什么事、在哪个文件上、对用户意味着什么。
	for _, want := range []string{"哈希缓存不可用", "cache.db", "速度会变慢", "不影响去重结果"} {
		if !strings.Contains(notice, want) {
			t.Fatalf("提示缺少「%s」这一层信息：%q", want, notice)
		}
	}
}

// 槽位语义：先发生的不许被后发生的挤掉。
// 两条同时触发是最常见的现场（配置目录被某个同步盘搅坏，两个库都打不开），
// 旧的"直接赋值"会让用户只看到后一条 ⇒ 修好一个又冒出一个，永远差一步。
func TestStartupNoticesAccumulate(t *testing.T) {
	a := newTestApp(t)
	breakCache(t, a.cfgDir)
	junk := []byte("garbage where an undo ledger used to live")
	if err := os.WriteFile(filepath.Join(a.cfgDir, "history.db"), junk, 0o644); err != nil {
		t.Fatal(err)
	}

	// 与 startup 的调用顺序一致：缓存先、账本后（§9.5-8）
	a.openCache()
	a.openLedger()

	a.mu.Lock()
	hs := a.hist
	a.mu.Unlock()
	if hs == nil {
		t.Fatal("账本确证损坏应隔离重建并留下句柄")
	}
	defer hs.Close()

	notice := a.GetStartupNotice()
	cacheAt := strings.Index(notice, "哈希缓存不可用")
	ledgerAt := strings.Index(notice, "影像损坏")
	if cacheAt < 0 || ledgerAt < 0 {
		t.Fatalf("两条提示应同时在场（缓存@%d 账本@%d）：%q", cacheAt, ledgerAt, notice)
	}
	if cacheAt > ledgerAt {
		t.Fatalf("累积顺序应与发生顺序一致（先缓存后账本）：%q", notice)
	}
	if !strings.Contains(notice, "\n") {
		t.Fatalf("两条之间应有分隔（GetStartupNotice 形状不变，以 \\n 分隔）：%q", notice)
	}
}

// 负控制：健康路径不许留提示。
// 每次都弹的假警会被用户脱敏，之后真警也跟着失效——这与 M25 要修的问题同样有害。
func TestOpenCacheQuietWhenHealthy(t *testing.T) {
	a := newTestApp(t)

	a.openCache() // 库不存在 → 正常新建
	if got := a.GetStartupNotice(); got != "" {
		t.Fatalf("新建缓存库不该有启动提示：%q", got)
	}
	a.mu.Lock()
	cch := a.cch
	a.mu.Unlock()
	if cch == nil {
		t.Fatal("健康路径应留下可用句柄")
	}
	cch.Close()

	a.mu.Lock()
	a.cch = nil
	a.mu.Unlock()
	a.openCache() // 第二次打开已有健康库
	if got := a.GetStartupNotice(); got != "" {
		t.Fatalf("健康库重开不该有提示：%q", got)
	}
	a.mu.Lock()
	cch2 := a.cch
	a.mu.Unlock()
	if cch2 == nil {
		t.Fatal("重开健康库应留下可用句柄")
	}
	cch2.Close()
}
