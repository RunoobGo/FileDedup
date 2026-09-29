package main

// M349 快照前置门槛 + M350「清缓存不连坐账本」边界守卫（设计段 §3.3 / §4.2）。
//
// 这两条都不是"修 bug"，是**把用户的裁定变成拦得住下一批改动**的东西：
//   - R-1/R-3 说"清缓存只移哈希、账本保留、清之前先留最近一份快照"。今天它巧合成立
//     （CacheClear 没去碰 a.hist 纯属没写），没有任何一条测试拦着"下次有人在清缓存里
//     顺手清历史"或"把快照挪到删之后再拍"。没有守卫的承诺会被下一批改坏。
//   - 门槛方向的判据是"快照失败 ⇒ 一条都不删"，不是"删了之后补一份"。

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/cache"
	"filededup/internal/fsid"
	"filededup/internal/history"
	"filededup/internal/model"
)

// clearFixture 带真实缓存 + 真实账本的 App（两个库都落 t.TempDir，互不相干）。
func clearFixture(t *testing.T) (*App, *cache.Cache) {
	t.Helper()
	a, _ := newHistApp(t) // 已经备好 a.cfgDir / a.hist / a.emit 桩
	cch, err := cache.Open(filepath.Join(a.cfgDir, "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cch.Close() })
	a.cch = cch
	return a, cch
}

// seedCache 灌 n 条带 full 的条目。
func seedCache(t *testing.T, c *cache.Cache, n int) {
	t.Helper()
	entries := make([]cache.Entry, 0, n)
	for i := 0; i < n; i++ {
		full := make([]byte, 32)
		entries = append(entries, cache.Entry{
			Path: fmt.Sprintf("/very/long/path/to/file/number/%06d/name.bin", i),
			Size: uint64(i * 1024), MtimeNs: int64(i),
			Head: uint64(i), Tail: uint64(i * 3), Full: full,
		})
	}
	if err := c.Store(entries); err != nil {
		t.Fatal(err)
	}
}

// seedLedgerRows 在账本里落 1 条扫描历史 + 1 笔清理记录（含 items）。
func seedLedgerRows(t *testing.T, hs *history.Store) {
	t.Helper()
	if _, err := hs.SaveScan(model.ScanConfig{Roots: []string{"/seed/root"}},
		[]*model.DuplicateGroup{mkGroup(1, 1024, "/seed/root/a.bin", "/seed/root/b.bin")}, nil); err != nil {
		t.Fatal(err)
	}
	var h [32]byte
	opID, err := hs.BeginOp("trash", "/seed/root", 1, true,
		[]history.OpItemPlan{{OrigPath: "/seed/root/b.bin", Hash: h, Size: 1024, MtimeNs: 7}})
	if err != nil {
		t.Fatal(err)
	}
	if err := hs.FinishItem(opID, "/seed/root/b.bin", "", "", history.StateDone, ""); err != nil {
		t.Fatal(err)
	}
	if err := hs.FinalizeOp(opID, 1024); err != nil {
		t.Fatal(err)
	}
}

// ---- M349：快照是前置门槛 ----

// 快照失败 ⇒ 清空必须整个取消，一条缓存都不许少。
func TestCacheClearAbortsWhenSnapshotFails(t *testing.T) {
	restore := cacheClearSnapshot
	defer func() { cacheClearSnapshot = restore }()
	cacheClearSnapshot = func(c *cache.Cache, dest string) error {
		return errors.New("模拟 VACUUM INTO 失败")
	}

	a, cch := clearFixture(t)
	seedCache(t, cch, 3000)
	before, err := a.CacheStats()
	if err != nil {
		t.Fatal(err)
	}
	res, err := a.CacheClear()
	if err == nil {
		t.Fatal("M349：快照失败却报告清空成功 —— 门槛没生效")
	}
	if !strings.Contains(err.Error(), "未删除任何缓存数据") {
		t.Errorf("M349：失败文案没写明「什么都没删」，用户会以为缓存已经没了：%v", err)
	}
	after, err := a.CacheStats()
	if err != nil {
		t.Fatal(err)
	}
	if after.Entries != before.Entries {
		t.Errorf("M349：快照失败后条目被删了 —— before=%d after=%d", before.Entries, after.Entries)
	}
	if res.EntriesCleared != 0 {
		t.Errorf("M349：取消路径仍回执了清除条数（=%d），前端会报成成功", res.EntriesCleared)
	}
	// 失败时不得留下半截 tmp 文件
	matches, _ := filepath.Glob(filepath.Join(a.cfgDir, "cache-backup.db*"))
	for _, m := range matches {
		t.Errorf("M349：快照失败后残留了 %s（旧快照不该被牵连，tmp 该清干净）", m)
	}
}

// 连清两次：固定名 ⇒ 盘上只有一份快照、无 tmp 残留（R-3 的字面落地）。
func TestCacheClearKeepsSingleSnapshot(t *testing.T) {
	a, cch := clearFixture(t)
	for round := 1; round <= 2; round++ {
		seedCache(t, cch, 500)
		res, err := a.CacheClear()
		if err != nil {
			t.Fatalf("第 %d 轮清空报错: %v", round, err)
		}
		if want := filepath.Join(a.cfgDir, CacheClearSnapshotName); res.SnapshotPath != want {
			t.Fatalf("第 %d 轮快照路径 = %s，want %s", round, res.SnapshotPath, want)
		}
		matches, _ := filepath.Glob(filepath.Join(a.cfgDir, "cache-backup.db*"))
		if len(matches) != 1 {
			t.Fatalf("第 %d 轮后快照文件应只有 1 份，实际 %v", round, matches)
		}
	}
}

// 回执三个数都得是真的：清了几条、回收多少字节、快照能不能打开读回原条目。
func TestCacheClearReportsRealCountsAndUsableSnapshot(t *testing.T) {
	a, cch := clearFixture(t)
	seedCache(t, cch, 20000)
	before, err := a.CacheStats()
	if err != nil {
		t.Fatal(err)
	}
	res, err := a.CacheClear()
	if err != nil {
		t.Fatalf("清空报错: %v", err)
	}
	if res.EntriesCleared != before.Entries || res.EntriesCleared != 20000 {
		t.Errorf("M349：回执条数失真 before=%d 回执=%d", before.Entries, res.EntriesCleared)
	}
	// 判据取 1 MB（实测这一档夹具回收 8.6 MB→24 KB 量级，留足平台余量）
	if res.ReclaimedBytes < 1<<20 {
		t.Errorf("M349：回收字节数没报或报小了（=%d），用户仍无从反证「占用为什么变了」", res.ReclaimedBytes)
	}
	// 快照可用性必须是断言，不能只是"文件生成了"
	sc, err := cache.Open(res.SnapshotPath)
	if err != nil {
		t.Fatalf("M349：快照打不开（= 这份备份防不了误删）: %v", err)
	}
	defer sc.Close()
	sst, err := sc.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	if sst.Entries != 20000 {
		t.Errorf("M349：快照里条目数=%d，want 20000（影像不完整）", sst.Entries)
	}
	if _, hit, _ := sc.Lookup("/very/long/path/to/file/number/000042/name.bin", 42*1024, 42, fsid.ID{}); !hit {
		t.Error("M349：快照逐条命中失败，影像不可用")
	}
}

// 回收失败那一腿（跨进程占 WAL）：条目已删、快照已落，回执仍要给数，
// 错误串仍要带着 ErrReclaimFailed 的身份 —— 不许说成"清空失败"。
//
// ★ 这一格刻意**不用接缝**，用第二个真实连接把 -wal 顶住（实测 busy 腿要
//
//	"读者持旧快照 + 本库继续写"两个条件同时成立才触发）。
func TestCacheClearReclaimFailStillReports(t *testing.T) {
	a, cch := clearFixture(t)
	seedCache(t, cch, 800)

	db2, err := sql.Open("sqlite", cch.DBPath())
	if err != nil {
		t.Skip("本机无 sqlite 驱动可用于第二连接: ", err)
	}
	defer db2.Close()
	rows, err := db2.Query(`SELECT path FROM hash_cache`)
	if err != nil {
		t.Skip("第二连接起不来读事务: ", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("夹具前提走样：读快照一个游标都起不来")
	}
	seedCache(t, cch, 300) // 推进 WAL，让 TRUNCATE 截不动

	res, err := a.CacheClear()
	switch {
	case err == nil:
		t.Skip("本次 busy 没复现（SQLite 把回收做完了），本格读数交回 CI 腿")
	case errors.Is(err, cache.ErrReclaimFailed):
		// 预期档：哨兵身份保住
	default:
		t.Fatalf("回收失败没落到 ErrReclaimFailed 上，上游将无法分岔文案: %v", err)
	}
	if res.EntriesCleared == 0 {
		t.Errorf("M349：回收失败被当成整次失败，回执条数为 0（条目其实已删净）")
	}
	// ★ reject 路径的契约：前端在 err != nil 时只收得到这句话，res 会被丢掉，
	//   所以条数必须同时写在错误串里，否则用户看到的就是一句没有数的"失败"。
	if err != nil && !strings.Contains(err.Error(), fmt.Sprintf("已清空 %d 条", res.EntriesCleared)) {
		t.Errorf("M349：错误串没带条数（reject 路径唯一可达的半截真话丢了）：%v", err)
	}
	if strings.Contains(err.Error(), "清空缓存失败") {
		t.Errorf("M349：文案把「已删净、只差空间」说成了整次失败：%v", err)
	}
	st, serr := a.CacheStats()
	if serr != nil {
		t.Fatal(serr)
	}
	if st.Entries != 0 {
		t.Errorf("M349：条目本该已删净，实际剩 %d 条", st.Entries)
	}
}

// ---- M350：清缓存不连坐账本 ----

// R-1 的执行合同。变异：在 CacheClear 里临时加一句 a.hist.ClearScans() ⇒ 本条必须红。
func TestCacheClearLeavesLedgerUntouched(t *testing.T) {
	a, cch := clearFixture(t)
	seedLedgerRows(t, a.hist)
	hsPath := filepath.Join(a.cfgDir, "history.db")

	beforeScans, err := a.ListScanHistory()
	if err != nil {
		t.Fatal(err)
	}
	beforeOps, err := a.ListOpRecords()
	if err != nil {
		t.Fatal(err)
	}
	if len(beforeScans) == 0 || len(beforeOps) == 0 {
		t.Fatal("夹具前提走样：账本没种出记录，本守卫无从拦")
	}
	hstBefore, err := os.Stat(hsPath)
	if err != nil {
		t.Fatal(err)
	}
	// 夹具前提：干净目录里不该已有隔离残留（下面 ④ 格要靠它）
	if m, _ := filepath.Glob(filepath.Join(a.cfgDir, "*.broken-*")); len(m) != 0 {
		t.Fatalf("夹具前提走样：干净目录里已有隔离残留 %v", m)
	}

	seedCache(t, cch, 2000)
	if _, err := a.CacheClear(); err != nil {
		t.Fatalf("清空报错: %v", err)
	}

	// ① 缓存确实清了（不然这条守卫是在测"什么都没发生"）
	if st, _ := a.CacheStats(); st.Entries != 0 {
		t.Fatalf("M350：缓存没清干净（剩 %d 条），本守卫的对照失效", st.Entries)
	}
	// ② 两张清单**逐条等值**（不是"非空"）
	afterScans, err := a.ListScanHistory()
	if err != nil {
		t.Fatal(err)
	}
	if len(afterScans) != len(beforeScans) {
		t.Errorf("M350：清缓存动了扫描历史 —— before=%d after=%d", len(beforeScans), len(afterScans))
	}
	if afterScans[0].Files != beforeScans[0].Files || afterScans[0].ID != beforeScans[0].ID {
		t.Errorf("M350：扫描历史行内容变了 —— before=%+v after=%+v", beforeScans[0], afterScans[0])
	}
	afterOps, err := a.ListOpRecords()
	if err != nil {
		t.Fatal(err)
	}
	if len(afterOps) != len(beforeOps) || afterOps[0].Done != beforeOps[0].Done {
		t.Errorf("M350：清缓存动了清理记录 —— before=%+v after=%+v", beforeOps[0], afterOps[0])
	}
	// ③ 账本文件的回撤能力仍在原处：明细可查、状态没被改写
	det, err := a.GetOpRecord(beforeOps[0].ID)
	if err != nil {
		t.Fatalf("M350：清理记录明细读不出了: %v", err)
	}
	if len(det.Items) == 0 {
		t.Error("M350：清理记录的条目全没了（回撤清单被连坐）")
	}
	// ④ 快照只落在缓存档，账本目录里不许多出它的兄弟
	if m, _ := filepath.Glob(hsPath + "*"); len(m) > 3 { // history.db(+wal/+shm)
		t.Errorf("M350：账本侧多出了文件 %v（快照或回收动作越界）", m)
	}
	// ⑤ 账本文件的字节数不许被缓存的 VACUUM 搅动
	hstAfter, err := os.Stat(hsPath)
	if err != nil {
		t.Fatal(err)
	}
	if hstAfter.Size() != hstBefore.Size() {
		t.Errorf("M350：清缓存改变了 history.db 的体积 —— before=%d after=%d", hstBefore.Size(), hstAfter.Size())
	}
}
