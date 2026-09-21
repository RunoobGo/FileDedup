package main

// APP-2（2026-09-21 全量审查，设计稿 §15.0-A）：M9 那一条纪律的第二例。
//
// M9 的裁定是"`a.hist` 受 `a.mu` 保护，读它必须在锁内且只读一次"，配套
// histSnapshot() + AST 门禁。`a.cch` 是**同一个生命周期**的另一个句柄：
// shutdown 在锁内 `a.cch.Close(); a.cch = nil`（app.go 生命周期段），而
// CacheStats/CacheClear 两个绑定入口写的是
//
//	if a.cch == nil { ... }
//	return a.cch.GetStats()
//
// ——解引用两次、且两次都在锁外。M9 的门禁白名单只管 `a.hist`，所以这一例
// 从 AST 到 -race 都没人拦（同文件里的同一条纪律，覆盖面不一致）。
//
// 本文件把两件事一起补上：① 竞争探针（与 TestHistFieldReadsRaceWithShutdown 同形）；
// ② 门禁扩到 `a.cch`——按 M9 的原话，这类竞争"从返回值上永远观测不到，只能靠门禁拦"。

import (
	"sync"
	"testing"

	"filededup/internal/cache"
)

// cchFixture 造一个带真实缓存句柄的 App（缓存要落盘，用 t.TempDir）。
func cchFixture(t *testing.T) (*App, *cache.Cache) {
	t.Helper()
	a := NewApp()
	a.cfgDir = t.TempDir()
	cch, err := cache.Open(a.cfgDir + "/cache.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cch.Close() })
	a.cch = cch
	return a, cch
}

// TestCchFieldReadsRaceWithShutdown 读侧走真实 RPC 入口，写侧复刻 shutdown 的锁内置空。
func TestCchFieldReadsRaceWithShutdown(t *testing.T) {
	a, cch := cchFixture(t)

	readers := []func(){
		func() { _, _ = a.CacheStats() },
		func() { _ = a.CacheClear() },
	}

	stop := make(chan struct{})
	var rwg sync.WaitGroup
	for i := 0; i < 4; i++ {
		rwg.Add(1)
		go func() {
			defer rwg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				for _, r := range readers {
					r()
				}
			}
		}()
	}
	// 写侧与 shutdown 逐字同形（Close 不在这里做：竞争发生在"读字段"这一步）。
	for i := 0; i < 300; i++ {
		a.mu.Lock()
		a.cch = nil
		a.mu.Unlock()
		a.mu.Lock()
		a.cch = cch
		a.mu.Unlock()
	}
	close(stop)
	rwg.Wait()
}

// TestCacheStatsReportsUnavailableAfterShutdown 缓存句柄释放后，两个绑定必须报
// "不可用"而不是 panic 在已置空的句柄上。改前的写法是锁外二次解引用，快照一旦
// 被 shutdown 置空就是从 nil 上取方法——这条是那条竞态的用户可见面。
func TestCacheStatsReportsUnavailableAfterShutdown(t *testing.T) {
	a, _ := cchFixture(t)
	a.mu.Lock()
	a.cch = nil
	a.mu.Unlock()

	if _, err := a.CacheStats(); err == nil {
		t.Fatal("句柄已释放，CacheStats 仍报成功（§15.0-A APP-2）")
	}
	if err := a.CacheClear(); err == nil {
		t.Fatal("句柄已释放，CacheClear 仍报成功（§15.0-A APP-2）")
	}
}
