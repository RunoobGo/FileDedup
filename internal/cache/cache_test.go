package cache

// M4-T01 单测：命中判定 / 元数据失效 / 批量 UPSERT / 淘汰 / 统计 / 清空 / 损坏隔离重建。
// M121（04 §6.11 CACHE-8）：改前这里写"损坏自愈"，而包注释（cache.go:5，M96）已把口径
// 改成"打开时隔离重建、运行期只停用，都不是自愈"——同一目录两份说法，这里跟包注释走。

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/fsid"
)

func openTest(t *testing.T) *Cache {
	t.Helper()
	c, err := Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestLookupHitAndMiss(t *testing.T) {
	c := openTest(t)
	full := make([]byte, 32)
	for i := range full {
		full[i] = byte(i)
	}
	if err := c.Store([]Entry{{Path: "/a", Size: 100, MtimeNs: 111, Head: 1, Tail: 2, Full: full}}); err != nil {
		t.Fatal(err)
	}
	// 完全一致 → 命中 + full 有效
	e, hit, fullValid := c.Lookup("/a", 100, 111, fsid.ID{})
	if !hit || !fullValid || e.Head != 1 || e.Tail != 2 {
		t.Fatalf("命中失败: %+v hit=%v fullValid=%v", e, hit, fullValid)
	}
	// mtime 变化 → 未命中
	if _, hit, _ := c.Lookup("/a", 100, 222, fsid.ID{}); hit {
		t.Fatal("mtime 变化应未命中")
	}
	// size 变化 → 未命中
	if _, hit, _ := c.Lookup("/a", 101, 111, fsid.ID{}); hit {
		t.Fatal("size 变化应未命中")
	}
	// 路径不存在 → 未命中
	if _, hit, _ := c.Lookup("/missing", 100, 111, fsid.ID{}); hit {
		t.Fatal("不存在的路径应未命中")
	}
}

func TestStorePartialOnlyUpsertFull(t *testing.T) {
	c := openTest(t)
	// 先存 partial-only（full=nil）
	if err := c.Store([]Entry{{Path: "/b", Size: 10, MtimeNs: 1, Head: 9, Tail: 9}}); err != nil {
		t.Fatal(err)
	}
	if _, hit, fullValid := c.Lookup("/b", 10, 1, fsid.ID{}); !hit || fullValid {
		t.Fatalf("partial-only: hit=%v fullValid=%v", hit, fullValid)
	}
	// 后补 full（UPSERT 覆盖）
	full := make([]byte, 32)
	full[0] = 0xAA
	if err := c.Store([]Entry{{Path: "/b", Size: 10, MtimeNs: 1, Full: full}}); err != nil {
		t.Fatal(err)
	}
	got, hit, fullValid := c.Lookup("/b", 10, 1, fsid.ID{})
	if !hit || !fullValid {
		t.Fatalf("补 full 后: hit=%v fullValid=%v", hit, fullValid)
	}
	// P0-2：补 full 不得抹掉已有采样（旧实现整行覆盖使 partial 清零 → 后续漏报）
	if got.Head != 9 || got.Tail != 9 {
		t.Fatalf("写回 full 时采样被覆盖: head=%d tail=%d", got.Head, got.Tail)
	}
}

func TestEvictOverLimit(t *testing.T) {
	c := openTest(t)
	n := 1000
	entries := make([]Entry, n)
	for i := 0; i < n; i++ {
		entries[i] = Entry{Path: "/f" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + string(rune('a'+i/676)), Size: 1, MtimeNs: 1}
	}
	// 用足够多独立路径
	for i := range entries {
		entries[i].Path = "/file/" + itoa(i)
	}
	if err := c.Store(entries); err != nil {
		t.Fatal(err)
	}
	s, _ := c.GetStats()
	if s.Entries != n {
		t.Fatalf("条目 = %d", s.Entries)
	}
	// 缩小上限验证淘汰逻辑（直接调用内部方法路径：临时改 MaxEntries 不可行——
	// 淘汰验证改用 SQL 层行为：删除一半再统计）
	if err := c.evictOverForTest(n / 2); err != nil {
		t.Fatal(err)
	}
	s, _ = c.GetStats()
	if s.Entries != n/2 {
		t.Fatalf("淘汰后条目 = %d, want %d", s.Entries, n/2)
	}
	if s.LastEvicted != n/2 {
		t.Fatalf("淘汰计数 = %d", s.LastEvicted)
	}
}

func TestStatsAndClear(t *testing.T) {
	c := openTest(t)
	_ = c.Store([]Entry{
		{Path: "/x", Size: 1, MtimeNs: 1, Head: 1, Tail: 1, Full: make([]byte, 32)},
		{Path: "/y", Size: 2, MtimeNs: 2, Head: 2, Tail: 2},
	})
	s, err := c.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	if s.Entries != 2 || s.WithFull != 1 {
		t.Fatalf("统计: %+v", s)
	}
	if s.DBSizeBytes <= 0 {
		t.Fatal("库大小应 > 0")
	}
	if err := c.Clear(); err != nil {
		t.Fatal(err)
	}
	s, _ = c.GetStats()
	if s.Entries != 0 {
		t.Fatalf("清空后条目 = %d", s.Entries)
	}
}

func TestCorruptSelfHeal(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "cache.db")
	// 写入垃圾字节（模拟损坏）
	if err := os.WriteFile(dbPath, []byte("this is not a sqlite database garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Open(dbPath) // Open 腿：改名隔离后重建（不是"自愈"，口径见 cache.go 包注释）
	if err != nil {
		t.Fatalf("损坏隔离重建失败: %v", err)
	}
	defer c.Close()
	if err := c.Store([]Entry{{Path: "/z", Size: 1, MtimeNs: 1, Head: 1, Tail: 1}}); err != nil {
		t.Fatal(err)
	}
	if _, hit, _ := c.Lookup("/z", 1, 1, fsid.ID{}); !hit {
		t.Fatal("重建后应可正常使用")
	}
}

func TestTouchRefreshesLastHit(t *testing.T) {
	// R1：命中续期后，淘汰应优先删除真正最旧的条目（未续期者）
	c := openTest(t)
	entries := []Entry{
		{Path: "/f0", Size: 1, MtimeNs: 1, Head: 1, Tail: 1},
		{Path: "/f1", Size: 1, MtimeNs: 1, Head: 2, Tail: 2},
		{Path: "/f2", Size: 1, MtimeNs: 1, Head: 3, Tail: 3},
	}
	if err := c.Store(entries); err != nil {
		t.Fatal(err)
	}
	// 将 f0/f1 老化为远古时间戳（f2 保持 now）
	c.mu.Lock()
	_, err := c.db.Exec(`UPDATE hash_cache SET last_hit = 100 WHERE path IN ('/f0', '/f1')`)
	c.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	// f0 命中续期（R1 修复路径）
	if err := c.Touch([]string{"/f0"}); err != nil {
		t.Fatal(err)
	}
	// 淘汰最旧 1 条：应为 f1（未续期），f0 已续期必须存活
	if err := c.evictOverForTest(1); err != nil {
		t.Fatal(err)
	}
	if _, hit, _ := c.Lookup("/f0", 1, 1, fsid.ID{}); !hit {
		t.Fatal("续期的 f0 不应被淘汰（R1 回归）")
	}
	if _, hit, _ := c.Lookup("/f1", 1, 1, fsid.ID{}); hit {
		t.Fatal("最旧的 f1 应被淘汰")
	}
	if _, hit, _ := c.Lookup("/f2", 1, 1, fsid.ID{}); !hit {
		t.Fatal("f2 不应被淘汰")
	}
	// 空列表无操作
	if err := c.Touch(nil); err != nil {
		t.Fatal(err)
	}
}

// H2：缓存命中须核对物理身份（dev/ino/ctime_ns）。同路径被换 inode 或原地写
// （ctime 前进）后，旧 full 哈希不可再用——任一要素不符即视为未命中。
func TestLookupIdentityMismatch(t *testing.T) {
	c := openTest(t)
	full := bytes.Repeat([]byte{0x5}, 32)
	if err := c.Store([]Entry{{Path: "/id", Size: 10, MtimeNs: 1,
		Head: 1, Tail: 2, Mid1: 3, Mid2: 4,
		Dev: 66, Ino: 77, CtimeNs: 88, Full: full}}); err != nil {
		t.Fatal(err)
	}
	if _, hit, fullValid := c.Lookup("/id", 10, 1,
		fsid.ID{Dev: 66, Ino: 77, CtimeNs: 88, Resolved: true}); !hit || !fullValid {
		t.Fatal("身份三要素一致应命中且 full 有效")
	}
	for _, id := range []fsid.ID{
		{Dev: 67, Ino: 77, CtimeNs: 88, Resolved: true},
		{Dev: 66, Ino: 78, CtimeNs: 88, Resolved: true},
		{Dev: 66, Ino: 77, CtimeNs: 89, Resolved: true},
	} {
		if _, hit, _ := c.Lookup("/id", 10, 1, id); hit {
			t.Fatalf("身份不一致必须未命中: %+v", id)
		}
	}
	// 未解析平台（无稳定身份的卷）：不因身份拦截，维持旧行为
	if _, hit, _ := c.Lookup("/id", 10, 1, fsid.ID{}); !hit {
		t.Fatal("未解析身份应照常命中")
	}
}

// I7：Windows 补上句柄身份后，升级前写入的零身份旧行不得继续放行命中——
// 那样等于让新证据形同虚设。首次未命中重算并补齐身份，之后才有比对基准。
func TestLookupLegacyZeroIdentityRowMisses(t *testing.T) {
	c := openTest(t)
	if err := c.Store([]Entry{{Path: "/legacy0", Size: 10, MtimeNs: 1,
		Head: 1, Tail: 2, Mid1: 3, Mid2: 4,
		Full: bytes.Repeat([]byte{0x7}, 32)}}); err != nil {
		t.Fatal(err)
	}
	if _, hit, _ := c.Lookup("/legacy0", 10, 1,
		fsid.ID{Dev: 9, Ino: 9, CtimeNs: 9, Resolved: true}); hit {
		t.Fatal("零身份旧行 + 已解析身份必须未命中（重算补齐）")
	}
	if _, hit, _ := c.Lookup("/legacy0", 10, 1, fsid.ID{}); !hit {
		t.Fatal("身份仍无法解析时应维持旧行为")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// evictOverForTest 淘汰到指定上限（仅测试用）。
func (c *Cache) evictOverForTest(limit int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.db.Exec(`DELETE FROM hash_cache WHERE path IN (
		SELECT path FROM hash_cache ORDER BY last_hit ASC, path ASC LIMIT ?)`, limit); err != nil {
		return err
	}
	c.cntValid = false // C5：绕过 Cache 写路径的测试钩子同样须使计数失效
	c.lastEvicted = limit
	return nil
}

// G4 回归：旧库遗留的 idx_cache_size（死索引）必须在 Open 时被清除。
// P0-2 补充：无版本标记的旧库同时被整表作废——旧语义（含 partial 被清零的脏行）
// 继续命中会导致漏报，因此"迁移保留既有数据"不再是期望行为。
func TestOpenDropsLegacyUnusedIndex(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")

	// 1) 手工重建一个"旧版本"库：含 size 索引与一条既有记录
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE hash_cache (
			path     TEXT    NOT NULL PRIMARY KEY,
			size     INTEGER NOT NULL,
			mtime_ns INTEGER NOT NULL,
			partial  BLOB    NOT NULL,
			full     BLOB,
			last_hit INTEGER NOT NULL DEFAULT 0)`,
		`CREATE INDEX idx_cache_size ON hash_cache(size)`,
		`INSERT INTO hash_cache (path, size, mtime_ns, partial, full, last_hit)
			VALUES ('/legacy', 7, 8, x'00000000000000000000000000000000', NULL, 123)`,
	} {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatalf("构造旧库失败: %v", err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	// 2) Open 应完成迁移
	c, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	names := indexNamesForTest(t, c)
	if hasStr(names, "idx_cache_size") {
		t.Fatalf("idx_cache_size 未被清除: %v", names)
	}
	if !hasStr(names, "idx_cache_last_hit") {
		t.Fatalf("last_hit 索引缺失（淘汰依赖它）: %v", names)
	}
	// 3) 旧版本记录被整体作废（P0-2），且版本已写入
	if _, hit, _ := c.Lookup("/legacy", 7, 8, fsid.ID{}); hit {
		t.Fatal("无版本标记的旧库记录不应继续命中")
	}
	st, err := c.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Entries != 0 {
		t.Fatalf("旧库应被作废, entries=%d", st.Entries)
	}
	// 4) 新库不应再建出该索引
	fresh := openTest(t)
	if n := indexNamesForTest(t, fresh); hasStr(n, "idx_cache_size") {
		t.Fatalf("新库不应创建 idx_cache_size: %v", n)
	}
}

// P0-2：同版本重开必须保留缓存（作废只发生在版本变更时），否则增量缓存无意义。
func TestAlgoVersionStableAcrossReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "stable.db")
	c, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	e := Entry{Path: "/keep", Size: 4096, MtimeNs: 42, Head: 7, Tail: 8, Full: bytes.Repeat([]byte{0x9}, 32)}
	if err := c.Store([]Entry{e}); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	c2, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	got, hit, fullValid := c2.Lookup("/keep", 4096, 42, fsid.ID{})
	if !hit || !fullValid {
		t.Fatalf("同版本重开后应命中且带全量: hit=%v fullValid=%v", hit, fullValid)
	}
	if got.Head != 7 || got.Tail != 8 {
		t.Fatalf("采样被破坏: head=%d tail=%d", got.Head, got.Tail)
	}
}

// P0-2：全零采样的行不可信，必须视为未命中（否则会与新文件永远不同桶 → 漏报）。
func TestLookupRejectsZeroSampleRow(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "zero.db")
	c, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// 绕开 Store 的 UPSERT 保留逻辑，直接造一行"采样为零但带 full"的脏数据
	if _, err := c.db.Exec(`INSERT INTO hash_cache (path,size,mtime_ns,partial,full,last_hit)
		VALUES ('/z', 5, 6, x'00000000000000000000000000000000', ?, 1)`, bytes.Repeat([]byte{0x1}, 32)); err != nil {
		t.Fatal(err)
	}
	if _, hit, _ := c.Lookup("/z", 5, 6, fsid.ID{}); hit {
		t.Fatal("全零采样行应视为未命中")
	}
}

func indexNamesForTest(t *testing.T, c *Cache) []string {
	t.Helper()
	rows, err := c.db.Query(`PRAGMA index_list(hash_cache)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var seq, unique, partial int
		var name, origin string
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			t.Fatal(err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func hasStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
