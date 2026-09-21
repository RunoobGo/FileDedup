package main

// M43 应用侧探针（2026-09-21，04 §6.8.8 M43；本轮审查 §14 兑现）：
// 账本打开失败必须走界面通道，不能只写 stderr。
//
// 后果比 M25 更重：账本不可用时回收站/移动/硬链接清理**被拒绝执行**（仅永久删除照常，
// 见 beginJournal），而 GUI 用户没有终端——只看到"点了没反应/报账本不可用"，无从自查。
//
// 夹具与 M25 同源：把 <cfgDir>/history.db 建成**目录**。这里不走"写垃圾"那条路——
// 垃圾会命中"确证损坏→隔离重建成功"（M12b 的分支），拿不到失败态。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/history"
)

func TestOpenLedgerUnavailableSurfacesNotice(t *testing.T) {
	a := newTestApp(t)
	histPath := filepath.Join(a.cfgDir, "history.db")
	if err := os.MkdirAll(histPath, 0o755); err != nil {
		t.Fatal(err)
	}
	// 夹具自证：失败必须真的发生，否则下面的"提示为空"红得没有意义。
	if _, err := history.Open(histPath); err == nil {
		t.Fatal("夹具失效：history.db 建成目录后 history.Open 仍成功，本用例没在测失败路径")
	}

	a.openLedger()

	a.mu.Lock()
	hs := a.hist
	a.mu.Unlock()
	if hs != nil {
		t.Fatal("夹具失效：openLedger 在失败路径上仍留下了句柄")
	}
	notice := a.GetStartupNotice()
	if notice == "" {
		t.Fatal("账本打开失败只写 stderr：GUI 无终端 ⇒ 用户看到的是「清理莫名被拒绝」，无法自查")
	}
	// 三件事缺一不可（照 cacheUnavailableNotice 的形状）：出了什么事、在哪个文件上、
	// 对用户意味着什么——最后这一层是 M43 相对 M25 的增量，少了就等于没修。
	for _, want := range []string{"历史库不可用", "history.db", "拒绝执行"} {
		if !strings.Contains(notice, want) {
			t.Fatalf("提示缺少「%s」这一层信息：%q", want, notice)
		}
	}
}

// 负控制：健康路径不许留提示（假警会被用户脱敏，之后真警跟着失效）。
func TestOpenLedgerQuietWhenHealthy(t *testing.T) {
	a := newTestApp(t)

	a.openLedger() // 库不存在 → 正常新建
	if got := a.GetStartupNotice(); got != "" {
		t.Fatalf("新建账本不该有启动提示：%q", got)
	}
	a.mu.Lock()
	hs := a.hist
	a.mu.Unlock()
	if hs == nil {
		t.Fatal("健康路径应留下可用句柄")
	}
	hs.Close()
}

// 与 M25 的累积槽共存：两条都在场、以 \n 分隔（GetStartupNotice 形状不变）。
func TestOpenLedgerNoticeAccumulatesWithCacheNotice(t *testing.T) {
	a := newTestApp(t)
	if err := os.MkdirAll(filepath.Join(a.cfgDir, "cache.db"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(a.cfgDir, "history.db"), 0o755); err != nil {
		t.Fatal(err)
	}

	a.openCache() // startup 的调用顺序：缓存先、账本后
	a.openLedger()

	notice := a.GetStartupNotice()
	cacheAt := strings.Index(notice, "哈希缓存不可用")
	ledgerAt := strings.Index(notice, "历史库不可用")
	if cacheAt < 0 || ledgerAt < 0 {
		t.Fatalf("两条提示应同时在场（缓存@%d 账本@%d）：%q", cacheAt, ledgerAt, notice)
	}
	if cacheAt > ledgerAt {
		t.Fatalf("累积顺序应与发生顺序一致（先缓存后账本）：%q", notice)
	}
	if !strings.Contains(notice, "\n") {
		t.Fatalf("两条之间应有分隔（\\n）：%q", notice)
	}
}
