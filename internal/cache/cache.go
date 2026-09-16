// Package cache 哈希缓存（04 M4-T01，01 §7.3）：
// SQLite WAL；键 (path, size, mtime_ns)；命中即复用，跳过读盘；
// 批量 UPSERT 单事务写回；last_hit 上限淘汰；损坏自愈（重建）。
package cache

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Entry 缓存条目：Partial 必有（预筛指纹），Full 可选（全量哈希，NULL 表示上次未算到）。
type Entry struct {
	Path    string
	Size    uint64
	MtimeNs int64
	Head    uint64 // xxHash64 头部 64KiB（小文件=全文件）
	Tail    uint64 // xxHash64 尾部 64KiB（小文件=全文件）
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

// Cache 线程安全缓存（单写多读；WAL 支持并发读）。
// Lookup 用 RLock 并行（阶段 2 多 worker 同时点查）；写操作独占。
type Cache struct {
	mu          sync.RWMutex
	db          *sql.DB
	path        string
	lastEvicted int
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
	return &Cache{db: db, path: path}, nil
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
			last_hit INTEGER NOT NULL DEFAULT 0
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

// Lookup 命中判定：path 存在且 size/mtime 完全一致。
// 返回条目与 full 是否有效（决定阶段 3 是否跳过）。
func (c *Cache) Lookup(path string, size uint64, mtimeNs int64) (Entry, bool, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	row := c.db.QueryRow(
		`SELECT size, mtime_ns, partial, full FROM hash_cache WHERE path = ?`, path)
	var e Entry
	var partial []byte
	e.Path = path
	if err := row.Scan(&e.Size, &e.MtimeNs, &partial, &e.Full); err != nil {
		return Entry{}, false, false
	}
	if e.Size != size || e.MtimeNs != mtimeNs {
		return Entry{}, false, false // 元数据变化 → 未命中（重算）
	}
	e.Head, e.Tail = decodePartial(partial)
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
	stmt, err := tx.Prepare(`INSERT INTO hash_cache (path, size, mtime_ns, partial, full, last_hit)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET size=excluded.size, mtime_ns=excluded.mtime_ns,
			partial=excluded.partial, full=excluded.full, last_hit=excluded.last_hit`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range entries {
		var full any
		if len(e.Full) == 32 {
			full = e.Full
		}
		if _, err := stmt.Exec(e.Path, e.Size, e.MtimeNs, encodePartial(e.Head, e.Tail), full, now); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
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

// evictLocked 超上限淘汰（last_hit 最旧优先）。
func (c *Cache) evictLocked() error {
	var n int
	if err := c.db.QueryRow(`SELECT COUNT(*) FROM hash_cache`).Scan(&n); err != nil {
		return err
	}
	if n <= MaxEntries {
		return nil
	}
	over := n - MaxEntries
	if _, err := c.db.Exec(`DELETE FROM hash_cache WHERE path IN (
		SELECT path FROM hash_cache ORDER BY last_hit ASC, path ASC LIMIT ?)`, over); err != nil {
		return err
	}
	c.lastEvicted = over
	return nil
}

// GetStats 统计。
func (c *Cache) GetStats() (Stats, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var s Stats
	if err := c.db.QueryRow(`SELECT COUNT(*), COUNT(full) FROM hash_cache`).Scan(&s.Entries, &s.WithFull); err != nil {
		return s, err
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
	return err
}

// Close 关闭。
func (c *Cache) Close() error { return c.db.Close() }

func encodePartial(head, tail uint64) []byte {
	b := make([]byte, 16)
	for i := 0; i < 8; i++ {
		b[i] = byte(head >> (8 * i))
		b[8+i] = byte(tail >> (8 * i))
	}
	return b
}

func decodePartial(b []byte) (head, tail uint64) {
	if len(b) != 16 {
		return 0, 0
	}
	for i := 0; i < 8; i++ {
		head |= uint64(b[i]) << (8 * i)
		tail |= uint64(b[8+i]) << (8 * i)
	}
	return head, tail
}
