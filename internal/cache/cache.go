// Package cache 哈希缓存（04 M4-T01，01 §7.3）：
// SQLite WAL；键 (path, size, mtime_ns, dev, ino, ctime_ns)；
// 命中后仍须四点采样比对，全一致才复用 full；
// 批量 UPSERT 单事务写回；last_hit 上限淘汰；损坏自愈（重建）。
package cache

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"filededup/internal/fsid"

	_ "modernc.org/sqlite"
)

// Entry 缓存条目：Partial 必有（预筛指纹），Full 可选（全量哈希，NULL 表示上次未算到）。
type Entry struct {
	Path    string
	Size    uint64
	MtimeNs int64
	Head    uint64 // xxHash64 头部 64KiB（小文件=全文件）
	Tail    uint64 // xxHash64 尾部 64KiB（小文件=全文件）
	Mid1    uint64 // xxHash64 size/2 处 64KiB（H1：中段采样）
	Mid2    uint64 // xxHash64 3size/4 处 64KiB
	Dev     uint64 // 物理身份（unix）；Windows 恒 0（fsid 未解析，比较平凡通过）
	Ino     uint64
	CtimeNs int64
	Full    []byte // BLAKE3-256；nil = 未算过全量（大文件预筛后被淘汰）
}

// Stats 缓存统计（CacheStats 契约）。
type Stats struct {
	Entries     int   `json:"entries"`
	WithFull    int   `json:"withFull"`
	DBSizeBytes int64 `json:"dbSizeBytes"`
	LastEvicted int   `json:"lastEvicted"`
}

// MaxEntries 条目上限（01 §7.3：默认 50 万）。
const MaxEntries = 500_000

// AlgoVersion 缓存语义版本（P0-2 / P2）。
//
// 命中判定只比 (path, size, mtime)，因此缓存行的含义一旦变化，旧行就可能
// 给出错误的分组依据。需要 bump 本版本的改动包括但不限于：
//   - 全量/采样哈希算法或长度（blake3-256 → 其他）
//   - hasher.HeadTailChunk / SmallFileMax 等采样参数
//   - Entry 字段语义（例如 partial 是否必须有效）
//
// 修正前无任何版本标记：升级或换算法后旧库继续命中，可能造出错误分组。
// 旧库还存在「阶段 3 写回把 partial 清零」留下的脏行（head=tail=0），
// 这些行与新算出的采样永远不同桶 → 漏报。bump 版本可一并清掉。
//
// v3（H1）：partial 从 2 点扩到 4 点（+size/2、+3size/4），并新增
// dev/ino/ctime_ns 身份列。旧行既缺中段采样又缺身份，一律作废。
const AlgoVersion = "blake3-256+xxh64-4pt+id-v3"

// metaKey 元信息表主键。
const metaKey = "algo_version"

// Cache 线程安全缓存（单写多读；WAL 支持并发读）。
// Lookup 用 RLock 并行（阶段 2 多 worker 同时点查）；写操作独占。
type Cache struct {
	mu          sync.RWMutex
	db          *sql.DB
	path        string
	lastEvicted int
	// C5/G11 触发式计数：条目数只在写入后变化（本进程独占此库），
	// 缓存计数并在任何写入后失效，避免每次统计/淘汰判定都全表 COUNT。
	cnt      int  // 条目总数（≈ COUNT(*)）
	cntFull  int  // 含全量哈希的条目数（≈ COUNT(full)）
	cntValid bool // cnt/cntFull 是否有效
}

// Open 打开或创建缓存库；损坏（无法打开/迁移）时自动重建（最坏退化为首扫速度）。
func Open(path string) (*Cache, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := openDB(path)
	if err != nil {
		// 损坏自愈：删除重建（丢失仅影响二次扫描速度，01 §11）
		os.Remove(path)
		os.Remove(path + "-wal")
		os.Remove(path + "-shm")
		db, err = openDB(path)
		if err != nil {
			return nil, fmt.Errorf("缓存库重建失败: %w", err)
		}
	}
	// P0-2 / P2：算法语义版本校验。版本不符（含旧库无版本标记）即整表作废，
	// 否则旧语义的缓存行会继续命中，可能导致错误分组或漏报。
	if err := enforceAlgoVersion(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Cache{db: db, path: path}, nil
}

// enforceAlgoVersion 比对并刷新缓存语义版本；不符则清空哈希表。
func enforceAlgoVersion(db *sql.DB) error {
	var got string
	err := db.QueryRow(`SELECT value FROM cache_meta WHERE key = ?`, metaKey).Scan(&got)
	if err == nil && got == AlgoVersion {
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("读取缓存版本失败: %w", err)
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// v3 起表结构含 dev/ino/ctime_ns 列；旧库缺列，DELETE 不足以让 schema
	// 追上写入语句，直接删表重建（enforce 后 openDB 的建表语句仍留在连接
	// 生命周期内首次执行，这里显式重放）。
	if _, err := tx.Exec(`DROP TABLE IF EXISTS hash_cache`); err != nil {
		return fmt.Errorf("作废旧缓存失败: %w", err)
	}
	if err := createSchema(tx); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO cache_meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, metaKey, AlgoVersion); err != nil {
		return fmt.Errorf("写入缓存版本失败: %w", err)
	}
	return tx.Commit()
}

// createSchema 哈希表结构（tx 版，供版本作废后重建；与 openDB 保持同构）。
func createSchema(tx *sql.Tx) error {
	for _, p := range []string{
		`CREATE TABLE IF NOT EXISTS hash_cache (
			path     TEXT    NOT NULL PRIMARY KEY,
			size     INTEGER NOT NULL,
			mtime_ns INTEGER NOT NULL,
			partial  BLOB    NOT NULL,
			full     BLOB,
			last_hit INTEGER NOT NULL DEFAULT 0,
			dev      INTEGER NOT NULL DEFAULT 0,
			ino      INTEGER NOT NULL DEFAULT 0,
			ctime_ns INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cache_last_hit ON hash_cache(last_hit)`,
	} {
		if _, err := tx.Exec(p); err != nil {
			return fmt.Errorf("重建缓存表失败: %w", err)
		}
	}
	return nil
}

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// PRAGMA（01 §7.3）
	for _, p := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		`CREATE TABLE IF NOT EXISTS hash_cache (
			path     TEXT    NOT NULL PRIMARY KEY,
			size     INTEGER NOT NULL,
			mtime_ns INTEGER NOT NULL,
			partial  BLOB    NOT NULL,
			full     BLOB,
			last_hit INTEGER NOT NULL DEFAULT 0,
			dev      INTEGER NOT NULL DEFAULT 0,
			ino      INTEGER NOT NULL DEFAULT 0,
			ctime_ns INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS cache_meta (
			key   TEXT NOT NULL PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		// G4：idx_cache_size 为死索引——size 从不作为查询谓词或排序键
		// （Lookup/Touch 走 path 主键，淘汰走 last_hit 索引），
		// 仅按行付出 B-tree 维护成本与库体积。旧库可能已建，故显式清除。
		`DROP INDEX IF EXISTS idx_cache_size`,
		`CREATE INDEX IF NOT EXISTS idx_cache_last_hit ON hash_cache(last_hit)`,
	} {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("初始化失败（疑似损坏）: %w", err)
		}
	}
	return db, nil
}

// Lookup 命中判定：path 存在且 size/mtime 与物理身份 (dev, ino, ctime_ns)
// 完全一致。id 未解析（Windows）时身份比较平凡通过，兜底仍靠四点采样。
// 返回条目与 full 是否有效（决定阶段 3 是否跳过）。
func (c *Cache) Lookup(path string, size uint64, mtimeNs int64, id fsid.ID) (Entry, bool, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	row := c.db.QueryRow(
		`SELECT size, mtime_ns, partial, full, dev, ino, ctime_ns FROM hash_cache WHERE path = ?`, path)
	var e Entry
	var partial []byte
	var dev64, ino64 int64
	e.Path = path
	if err := row.Scan(&e.Size, &e.MtimeNs, &partial, &e.Full, &dev64, &ino64, &e.CtimeNs); err != nil {
		return Entry{}, false, false
	}
	e.Dev, e.Ino = uint64(dev64), uint64(ino64)
	if e.Size != size || e.MtimeNs != mtimeNs {
		return Entry{}, false, false // 元数据变化 → 未命中（重算）
	}
	// H1：mtime 无法证明内容未变（粗粒度卷/原地改写保 mtime），ctime 与
	// inode 身份提供第二重证据：写内容、rename、chmod 都会推进 ctime；
	// 路径被换成另一文件则 dev/ino 必不同。不一致宁可当次未命中重算。
	if id.Resolved && (e.Dev != id.Dev || e.Ino != id.Ino || e.CtimeNs != id.CtimeNs) {
		return Entry{}, false, false
	}
	e.Head, e.Tail, e.Mid1, e.Mid2 = decodePartial(partial)
	// P0-2 防御：采样为全零的行不可信（历史写入路径曾把 partial 覆盖为零，
	// 见 enforceAlgoVersion 的说明）。这类行若参与分桶会让文件与真正的同内容
	// 文件永远不同桶，表现为漏报；宁可当作未命中重算一次。
	if e.Head == 0 && e.Tail == 0 && e.Mid1 == 0 && e.Mid2 == 0 {
		return Entry{}, false, false
	}
	fullValid := len(e.Full) == 32
	// 异步更新 last_hit 会引入写竞争；此处随批量写回刷新（够用）
	return e, true, fullValid
}

// Store 批量 UPSERT（任务结束单事务写回，01 §7.3）。
// last_hit 统一刷新为当前时间；超上限时按 last_hit 淘汰最旧。
func (c *Cache) Store(entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().Unix()
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT INTO hash_cache (path, size, mtime_ns, partial, full, last_hit, dev, ino, ctime_ns)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET size=excluded.size, mtime_ns=excluded.mtime_ns,
			partial=CASE WHEN excluded.partial = x'0000000000000000000000000000000000000000000000000000000000000000'
				THEN hash_cache.partial ELSE excluded.partial END,
			full=excluded.full, last_hit=excluded.last_hit,
			dev=excluded.dev, ino=excluded.ino, ctime_ns=excluded.ctime_ns`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range entries {
		var full any
		if len(e.Full) == 32 {
			full = e.Full
		}
		if _, err := stmt.Exec(e.Path, e.Size, e.MtimeNs,
			encodePartial(e.Head, e.Tail, e.Mid1, e.Mid2), full, now,
			int64(e.Dev), int64(e.Ino), e.CtimeNs); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	c.cntValid = false // C5：写入后计数失效，下次统计/淘汰判定重算一次
	return c.evictLocked()
}

// Touch 批量刷新命中条目的 last_hit（LRU 语义：命中即续期）。
// R1 修复：此前命中条目永不续期，高频命中的热文件反而最先被淘汰。
// 单事务执行；空列表无操作。
func (c *Cache) Touch(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().Unix()
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`UPDATE hash_cache SET last_hit = ? WHERE path = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, p := range paths {
		if _, err := stmt.Exec(now, p); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LastHit 读取条目 last_hit（诊断/测试用：验证命中续期语义）。
func (c *Cache) LastHit(path string) (int64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var ts int64
	if err := c.db.QueryRow(`SELECT last_hit FROM hash_cache WHERE path = ?`, path).Scan(&ts); err != nil {
		return 0, false
	}
	return ts, true
}

// evictLocked 超上限淘汰（last_hit 最旧优先）。调用方须持写锁。
func (c *Cache) evictLocked() error {
	// C5/G11：优先用缓存计数，仅在失效（最近有写入）时全表 COUNT 一次
	if !c.cntValid {
		if err := c.db.QueryRow(`SELECT COUNT(*), COUNT(full) FROM hash_cache`).
			Scan(&c.cnt, &c.cntFull); err != nil {
			return err
		}
		c.cntValid = true
	}
	if c.cnt <= MaxEntries {
		return nil
	}
	over := c.cnt - MaxEntries
	if _, err := c.db.Exec(`DELETE FROM hash_cache WHERE path IN (
		SELECT path FROM hash_cache ORDER BY last_hit ASC, path ASC LIMIT ?)`, over); err != nil {
		return err
	}
	c.cnt -= over
	c.lastEvicted = over
	return nil
}

// GetStats 统计。读锁即可（C5：修正前取写锁，会阻塞并发的 Lookup 读）；
// 条目计数走触发式缓存（G11）：计数只在写锁内回写，写后由 evictLocked 即时
// 重算，因此常态下零全表扫描；仅在 Clear 之后的首查走一次 COUNT（不回写，
// 避免持读锁写共享字段与并发 GetStats 竞争）。
func (c *Cache) GetStats() (Stats, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var s Stats
	if c.cntValid {
		s.Entries = c.cnt
		s.WithFull = c.cntFull
	} else {
		if err := c.db.QueryRow(`SELECT COUNT(*), COUNT(full) FROM hash_cache`).
			Scan(&s.Entries, &s.WithFull); err != nil {
			return s, err
		}
	}
	s.LastEvicted = c.lastEvicted
	if st, err := os.Stat(c.path); err == nil {
		s.DBSizeBytes = st.Size()
	}
	return s, nil
}

// Clear 清空缓存。
func (c *Cache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.db.Exec(`DELETE FROM hash_cache`)
	c.cntValid = false // C5：清空后计数失效
	c.lastEvicted = 0
	return err
}

// Close 关闭。
func (c *Cache) Close() error { return c.db.Close() }

func encodePartial(head, tail, mid1, mid2 uint64) []byte {
	b := make([]byte, 32)
	for i := 0; i < 8; i++ {
		b[i] = byte(head >> (8 * i))
		b[8+i] = byte(tail >> (8 * i))
		b[16+i] = byte(mid1 >> (8 * i))
		b[24+i] = byte(mid2 >> (8 * i))
	}
	return b
}

func decodePartial(b []byte) (head, tail, mid1, mid2 uint64) {
	if len(b) != 32 {
		return 0, 0, 0, 0
	}
	for i := 0; i < 8; i++ {
		head |= uint64(b[i]) << (8 * i)
		tail |= uint64(b[8+i]) << (8 * i)
		mid1 |= uint64(b[16+i]) << (8 * i)
		mid2 |= uint64(b[24+i]) << (8 * i)
	}
	return head, tail, mid1, mid2
}
