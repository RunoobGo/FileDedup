package cache

// M74（04 §6.11 CACHE-3）探针：设计段 §18.1-3 / §18.3 P-18-3 / P-18-8。
//
// 改前的形状：Lookup 的 `row.Scan(...) err != nil ⇒ return Entry{}, false, false`
// 把 sql.ErrNoRows（正常的"这条没有"）与"库读不动了"折成同一个出口，于是
//   - 运行期损坏没有任何证据（只有 Store 每轮报一次）；
//   - 损坏判定只在 Open 跑过一次 ⇒ 库中途坏了永不自愈，下一轮照旧去撞。
// 改后仍保留"宁可当次未命中重算"的兜底行为（这一点不能改，改了会误分组），
// 但必须留下证据、确证损坏时停用。

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/dbfile"
	"filededup/internal/fsid"
	"filededup/internal/sqlconn"

	"modernc.org/sqlite"
)

// P-18-3：真实的 DB 错误必须留下计数，且**不得**被误判成损坏。
// 手段：把底层连接池关掉再点查——"sql: database is closed"是暂时性故障，
// 走 IsCorruption 判据必须是 false（停用的门槛比"出过错"高得多，这点要钉住）。
func TestLookupDBErrorLeavesEvidenceAndIsNotCalledCorruption(t *testing.T) {
	c := openTest(t)
	if err := c.Store([]Entry{{Path: "/a", Size: 10, MtimeNs: 1, Head: 3, Tail: 4}}); err != nil {
		t.Fatal(err)
	}
	if _, hit, _ := c.Lookup("/a", 10, 1, fsid.ID{}); !hit {
		t.Fatal("前提：/a 应命中")
	}
	if got := c.DBErrors(); got != 0 {
		t.Fatalf("正常命中/未命中不该记账，实测 DBErrors=%d", got)
	}
	// Close 掉 Cache 的句柄所有权：t.Cleanup 还会再 Close 一次，*sql.DB 幂等。
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, hit, _ := c.Lookup("/a", 10, 1, fsid.ID{}); hit {
		t.Error("库已不可用却报命中：行为兜底必须仍是未命中")
	}
	if got := c.DBErrors(); got != 1 {
		t.Errorf("DB 错误必须留证据：DBErrors=%d want 1", got)
	}
	if c.Corrupted() {
		t.Error("暂时性故障不得置停用位（停用判据只认确证损坏）")
	}
	// 兜底行为本身不许变：错误之后仍算未命中，且第二次点查继续记账（未停用 ⇒ 还会撞）。
	if _, hit, _ := c.Lookup("/a", 10, 1, fsid.ID{}); hit {
		t.Error("应仍判未命中")
	}
	if got := c.DBErrors(); got != 2 {
		t.Errorf("未停用时每次点查都应各记一笔：DBErrors=%d want 2", got)
	}
}

// P-18-3b：ErrNoRows 是正常未命中，一条都不能记。
// （过度修正的形状：把"没查到"也折进 dbErrs ⇒ 每轮扫几万条路径就刷几万笔假账。）
func TestMissingRowIsNotACorruptionError(t *testing.T) {
	c := openTest(t)
	if err := c.Store([]Entry{{Path: "/a", Size: 10, MtimeNs: 1, Head: 3, Tail: 4}}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/nope1", "/nope2", "/nope3"} {
		if _, hit, _ := c.Lookup(p, 10, 1, fsid.ID{}); hit {
			t.Fatalf("%s 不该命中", p)
		}
	}
	if got := c.DBErrors(); got != 0 {
		t.Errorf("未命中不是错误：DBErrors=%d want 0", got)
	}
	if c.Corrupted() {
		t.Error("未命中更不该触发停用")
	}
}

// newCacheOn 拿一个指向该影像、但**一条 SQL 都没发过**的 Cache 实例。
//
// 为什么不走 openDB/Open：两者都会先向文件发建表与 PRAGMA 语句，损坏影像在那一步
// 就被 IsCorruption 吃掉（Open 的隔离重建是既有能力，见 cache_dbfile_test.go），
// 到不了"运行期读到一半才坏"这条被检路径。这里只把句柄搭好，首次真正读表的
// 就是被测的那条 Lookup。
func newCacheOn(path string) (*Cache, error) {
	base, err := sqlite.NewConnector(path)
	if err != nil {
		return nil, err
	}
	return &Cache{db: sql.OpenDB(sqlconn.WithPragmas(base, connPragmas)), path: path}, nil
}

// corruptionEvidence 另开一个与实例无关的句柄去查同一张表，取回底层错误原文。
// 前提自检必须独立于被测物：这里证明的是"夹具确实造出了数据库层面的损坏"，
// 不是"被测对象自己说坏了"。
func corruptionEvidence(path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	var n int
	return db.QueryRow(`SELECT count(*) FROM hash_cache`).Scan(&n)
}

// P-18-8：确证损坏 ⇒ 停用，且停用后**不再向库发 SQL**。
func TestRuntimeCorruptionStopsIssuingSQL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.db")
	c, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Store([]Entry{{Path: "/a", Size: 10, MtimeNs: 1, Head: 3, Tail: 4}}); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil { // 放掉连接 ⇒ 页缓存随连接一起没了
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("this is not a database at all ", 200)), 0o644); err != nil {
		t.Fatal(err)
	}
	// 前提自检（先于一切断言）：影像必须真的过 IsCorruption，否则下面的断言
	// 只是在测判据本身，不是在测停用行为。
	raw := corruptionEvidence(path)
	if raw == nil {
		t.Fatal("夹具没造出错误：垃圾影像照样查得动，本用例前提不成立")
	}
	if !dbfile.IsCorruption(raw) {
		t.Fatalf("夹具的底层错误原文未过 dbfile.IsCorruption：%q", raw)
	}

	broken, err := newCacheOn(path)
	if err != nil {
		t.Fatalf("构造损坏夹具失败: %v", err)
	}
	defer broken.Close()
	if _, hit, _ := broken.Lookup("/a", 10, 1, fsid.ID{}); hit {
		t.Error("损坏库不得报命中")
	}
	if !broken.Corrupted() {
		t.Fatalf("SQLite 原文必须被认成损坏（说明这不是拿假错误凑的）；DBErrors=%d 底层错误=%v",
			broken.DBErrors(), raw)
	}
	seen := broken.DBErrors()
	for i := 0; i < 5; i++ {
		if _, hit, _ := broken.Lookup("/a", 10, 1, fsid.ID{}); hit {
			t.Fatal("损坏库不得报命中")
		}
	}
	if got := broken.DBErrors(); got != seen {
		t.Errorf("停用后不应再向库发 SQL：DBErrors 从 %d 涨到 %d", seen, got)
	}
	// 写侧同样短路，并给出可判定的停用错误。
	if err := broken.Store([]Entry{{Path: "/b", Size: 1, MtimeNs: 1, Head: 1, Tail: 1}}); !errors.Is(err, ErrCorruptDisabled) {
		t.Errorf("停用后 Store 应回 ErrCorruptDisabled，实测 %v", err)
	}
	if err := broken.Touch([]string{"/b"}); !errors.Is(err, ErrCorruptDisabled) {
		t.Errorf("停用后 Touch 应回 ErrCorruptDisabled，实测 %v", err)
	}
}
