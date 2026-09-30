package main

// 第八轮审查批 2（拟 M359 / M364）：两个维护口的双向互斥闸。
//
// ★ 本文件刻意**不引用**任何本批新增的字段或函数（`maintaining`、`SnapshotNote`、
// `landCacheSnapshot` 都不出现）：这样"修前必红"才落在**行为**上而不是落在编译错误上。
// 三条接缝（cacheClearSnapshot / cacheClearStats / ledgerClearOps）在写本文件之前已经
// 作为纯重构落地，所以暂停点、失败注入都是改前就存在的东西。
//
// 前提自检走 M93 那条教训：同一夹具在不占任何标记时必须**真的受理**扫描与清理请求，
// 否则"被拒"这个结果无法归因给被测门禁。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"filededup/internal/cache"
	"filededup/internal/history"
	"filededup/internal/model"
)

// operableAppWithCache 铺一个「结果集就绪 + 真实缓存 + 真实账本」的 App，
// 使 StartScan 与 ExecuteOperation 两条请求都能一路走到互斥门（而不是停在下游某道门）。
func operableAppWithCache(t *testing.T) (*App, *cache.Cache, uint64) {
	t.Helper()
	a, _, id := newOperableApp(t)
	cch, err := cache.Open(filepath.Join(a.cfgDir, "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cch.Close() })
	a.cch = cch
	return a, cch, id
}

func setScanInFlight(a *App, v bool) {
	a.mu.Lock()
	a.scanInFlight = v
	a.mu.Unlock()
}

// assertIdleAppAcceptsBoth 前提自检：空闲夹具必须同时受理扫描与清理。
func assertIdleAppAcceptsBoth(t *testing.T) {
	t.Helper()
	a, _, id := operableAppWithCache(t)
	if _, err := a.StartScan(model.ScanConfig{Roots: []string{a.cfgDir}, Threads: 2}); err != nil {
		t.Fatalf("前提自检失败：空闲 App 的扫描请求应被受理，实际被拒：%v（夹具穿不过互斥门下游的某道门 ⇒ 本文件的互斥断言无法归因给被测门禁）", err)
	}
	a.wg.Wait()
	a2, _, id2 := operableAppWithCache(t)
	if _, err := a2.ExecuteOperation(model.OpRequest{Kind: "trash", FileIDs: []uint64{id2}}); err != nil {
		t.Fatalf("前提自检失败：空闲 App 的可操作请求应被受理，实际被拒：%v", err)
	}
	_ = id
	a2.wg.Wait()
}

// ---- 正向闸：扫描/清理在途时清缓存必须被拒 ----

// P-11：清理/回撤在途 ⇒ 清缓存拒绝，且缓存条目一条不少。
func TestM359CacheClearRejectedWhileOpsRunning(t *testing.T) {
	a, cch, _ := operableAppWithCache(t)
	seedCache(t, cch, 40)
	before, err := cch.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	setOpsRunning(a, true)
	defer setOpsRunning(a, false)

	_, err = a.CacheClear()
	if err == nil {
		t.Fatal("M359：清理/回撤在途时清缓存被受理了（hash_cache 正被扫描与写回共用）")
	}
	if !strings.Contains(err.Error(), "执行中") {
		t.Errorf("M359：拒绝文案没点名在途原因：%v", err)
	}
	after, err := cch.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	if after.Entries != before.Entries {
		t.Errorf("M359：拒绝路径仍动了缓存 —— before=%d after=%d", before.Entries, after.Entries)
	}
}

// P-12：扫描在途 ⇒ 同上。
func TestM359CacheClearRejectedWhileScanInFlight(t *testing.T) {
	a, cch, _ := operableAppWithCache(t)
	seedCache(t, cch, 40)
	before, err := cch.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	setScanInFlight(a, true)
	defer setScanInFlight(a, false)

	_, err = a.CacheClear()
	if err == nil {
		t.Fatal("M359：扫描在途时清缓存被受理了（快照拍到的是半张表，回收还会撞上写回）")
	}
	if !strings.Contains(err.Error(), "扫描") {
		t.Errorf("M359：拒绝文案没点名扫描：%v", err)
	}
	after, _ := cch.GetStats()
	if after.Entries != before.Entries {
		t.Errorf("M359：拒绝路径仍动了缓存 —— before=%d after=%d", before.Entries, after.Entries)
	}
}

// pauseInSnapshot 把清缓存停在「落快照」这一步里，fn 在暂停点上跑（反向闸的公共骨架）。
func pauseInSnapshot(t *testing.T, a *App, fn func()) {
	t.Helper()
	restore := cacheClearSnapshot
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	cacheClearSnapshot = func(c *cache.Cache, dest string) error {
		once.Do(func() { close(entered) })
		<-release
		return restore(c, dest)
	}
	t.Cleanup(func() { cacheClearSnapshot = restore })

	done := make(chan error, 1)
	go func() {
		_, err := a.CacheClear()
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("M359：夹具前提走样——清缓存没能停在落快照那一步")
		return
	}
	fn()
	close(release)
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("M359：释放后清缓存没有收尾")
	}
}

// P-13：清缓存在途 ⇒ 新扫描必须被拒。
func TestM359StartScanRejectedWhileCacheClearing(t *testing.T) {
	assertIdleAppAcceptsBoth(t)
	a, cch, _ := operableAppWithCache(t)
	seedCache(t, cch, 30)

	pauseInSnapshot(t, a, func() {
		_, err := a.StartScan(model.ScanConfig{Roots: []string{a.cfgDir}, Threads: 2})
		if err == nil {
			t.Fatal("M359：清缓存期间新扫描被受理 —— 双向闸只关了一半")
		}
		if !strings.Contains(err.Error(), "清空缓存") {
			t.Errorf("M359：拒绝文案没点名是哪项维护（用户只看到一句无主的进行中）：%v", err)
		}
	})
}

// P-14：清缓存在途 ⇒ 清理请求必须被拒。
func TestM359ExecuteOperationRejectedWhileCacheClearing(t *testing.T) {
	a, cch, id := operableAppWithCache(t)
	seedCache(t, cch, 30)

	pauseInSnapshot(t, a, func() {
		_, err := a.ExecuteOperation(model.OpRequest{Kind: "trash", FileIDs: []uint64{id}})
		if err == nil {
			t.Fatal("M359：清缓存期间清理操作被受理 —— 双向闸只关了一半")
		}
		if !strings.Contains(err.Error(), "清空缓存") {
			t.Errorf("M359：拒绝文案没点名是哪项维护：%v", err)
		}
	})
}

// ---- P-22：清记录在途 ⇒ 两条请求都被拒（判+写同临界区） ----

// pauseInLedgerClear 把清空清理记录停在「整表删除」那一步里。
func pauseInLedgerClear(t *testing.T, a *App, fn func()) {
	t.Helper()
	restore := ledgerClearOps
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	ledgerClearOps = func(hs *history.Store) error {
		once.Do(func() { close(entered) })
		<-release
		return restore(hs)
	}
	t.Cleanup(func() { ledgerClearOps = restore })

	done := make(chan error, 1)
	go func() { done <- a.ClearOpRecords() }()
	<-entered
	fn()
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("M364：夹具的清空清理记录本身失败了：%v", err)
	}
}

func TestM364ScanAndOpsRejectedWhileRecordsClearing(t *testing.T) {
	assertIdleAppAcceptsBoth(t)
	a, cch, id := operableAppWithCache(t)
	seedCache(t, cch, 10)
	seedLedgerRows(t, a.hist)

	pauseInLedgerClear(t, a, func() {
		if _, err := a.StartScan(model.ScanConfig{Roots: []string{a.cfgDir}, Threads: 2}); err == nil {
			t.Error("M364：清空清理记录期间新扫描被受理 —— 判与写仍是两段")
		} else if !strings.Contains(err.Error(), "清空清理记录") {
			t.Errorf("M364：扫描侧拒绝文案没点名维护项：%v", err)
		}
		if _, err := a.ExecuteOperation(model.OpRequest{Kind: "trash", FileIDs: []uint64{id}}); err == nil {
			t.Error("M364：清空清理记录期间清理操作被受理 —— 新落的账会被这次整表删除抽走")
		} else if !strings.Contains(err.Error(), "清空清理记录") {
			t.Errorf("M364：清理侧拒绝文案没点名维护项：%v", err)
		}
	})
}

// P-20：清空已成立、只差回收字节没核对上 ⇒ 那半句真话必须进错误串。
//
// 改前真读数（本文件写下的预测）：错误串只剩一句 shell 壳，"已清空 N 条 · 快照在 X"整段丢失，
// 而 Wails 在 err != nil 时把结构体丢掉 ⇒ 用户看到的是一次"失败"，磁盘上缓存却已经空了。
func TestM362StatsFailureStillCarriesReceipt(t *testing.T) {
	a, cch := clearFixture(t)
	seedCache(t, cch, 120)

	restore := cacheClearStats
	var calls int
	cacheClearStats = func(c *cache.Cache) (cache.Stats, error) {
		calls++
		if calls >= 2 {
			return cache.Stats{}, errors.New("模拟统计读失败")
		}
		return restore(c)
	}
	t.Cleanup(func() { cacheClearStats = restore })

	res, err := a.CacheClear()
	if err == nil {
		t.Fatal("M362：夹具没让第二次统计失败（calls=" + fmt.Sprint(calls) + "），本格前提走样")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("已清空 %d 条", res.EntriesCleared)) {
		t.Errorf("M362：错误串没带条数（reject 路径唯一可达的半截真话丢了）：%v", err)
	}
	if !strings.Contains(err.Error(), "快照") {
		t.Errorf("M362：错误串没交代快照在哪（用户会以为连备份也没了）：%v", err)
	}
	if strings.Contains(err.Error(), "清空缓存失败") {
		t.Errorf("M362：把「已清净、只差核对」说成整次失败：%v", err)
	}
	if st, serr := cch.GetStats(); serr != nil || st.Entries != 0 {
		t.Errorf("M362：清空本该已经成立（entries=%d err=%v）", st.Entries, serr)
	}
	// 快照文件必须已经落位（这一步在统计之前，顺序不许反）
	if _, serr := os.Stat(res.SnapshotPath); serr != nil {
		t.Errorf("M362：统计失败那条臂把已经落位的快照牵连掉了（%s：%v）", res.SnapshotPath, serr)
	}
}
