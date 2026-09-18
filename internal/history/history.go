// Package history 扫描历史与清理操作日志的持久化（v0.5.0 功能 3/4）。
//
// 独立 history.db：与哈希缓存 cache.db 物理隔离——缓存有算法版本作废与损坏
// 自愈逻辑，若同库会把历史/回撤账本一并清空，属数据安全事故。
package history

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

// MaxScanHistory 扫描历史上限，超出淘汰最旧（CASCADE 清组/文件行）。
const MaxScanHistory = 20

// SchemaVersion 库结构版本（PRAGMA user_version）。
const SchemaVersion = 1

// op_items.state 取值（写前日志状态机，spec §7）。
const (
	StatePlanned     = "planned"
	StateDone        = "done"
	StateFailed      = "failed"
	StateSkipped     = "skipped"
	StateCancelled   = "cancelled"
	StateInterrupted = "interrupted"
	StateUndone      = "undone"
	StateUndoFailed  = "undo_failed"
)

// Store 线程安全历史库（单写多读；全部方法内部串行化）。
type Store struct {
	mu   sync.Mutex
	db   *sql.DB
	path string
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS scan_history (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  saved_at     INTEGER NOT NULL,
  roots        TEXT    NOT NULL,
  filters      TEXT    NOT NULL,
  threads      INTEGER NOT NULL DEFAULT 0,
  paranoid     INTEGER NOT NULL DEFAULT 0,
  groups_count INTEGER NOT NULL,
  files_count  INTEGER NOT NULL,
  orig_files   INTEGER NOT NULL,
  reclaimable  INTEGER NOT NULL,
  failed_json  TEXT    NOT NULL DEFAULT '[]',
  keep_paths   TEXT    NOT NULL DEFAULT '[]'
);
CREATE TABLE IF NOT EXISTS hist_groups (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  hist_id     INTEGER NOT NULL REFERENCES scan_history(id) ON DELETE CASCADE,
  hash        BLOB    NOT NULL,
  size        INTEGER NOT NULL,
  reclaimable INTEGER NOT NULL,
  ord         INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS hist_files (
  id       INTEGER PRIMARY KEY AUTOINCREMENT,
  hist_id  INTEGER NOT NULL REFERENCES scan_history(id) ON DELETE CASCADE,
  group_id INTEGER NOT NULL REFERENCES hist_groups(id) ON DELETE CASCADE,
  path     TEXT    NOT NULL,
  size     INTEGER NOT NULL,
  mtime_ns INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_hist_files_hist ON hist_files(hist_id, group_id);
CREATE INDEX IF NOT EXISTS idx_hist_files_path ON hist_files(hist_id, path);
CREATE TABLE IF NOT EXISTS op_records (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  op_kind     TEXT NOT NULL,
  created_at  INTEGER NOT NULL,
  target_dir  TEXT NOT NULL DEFAULT '',
  hist_id     INTEGER NOT NULL DEFAULT 0,
  undoable    INTEGER NOT NULL DEFAULT 0,
  done_count  INTEGER NOT NULL DEFAULT 0,
  reclaimed   INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS op_items (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  op_id      INTEGER NOT NULL REFERENCES op_records(id) ON DELETE CASCADE,
  orig_path  TEXT NOT NULL,
  dest_path  TEXT NOT NULL DEFAULT '',
  link_src   TEXT NOT NULL DEFAULT '',
  hash       BLOB    NOT NULL,
  size       INTEGER NOT NULL,
  mtime_ns   INTEGER NOT NULL,
  state      TEXT    NOT NULL,
  err        TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_op_items_op ON op_items(op_id, state);
`

// initConn 单连接：本库写入频率极低（扫描/清理各一次批量），
// 单连接从根上消除 SQLITE_BUSY 面，也让外键开关必然生效。
func initConn(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, p := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
		fmt.Sprintf("PRAGMA user_version=%d", SchemaVersion),
	} {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("初始化失败（疑似损坏）: %w", err)
		}
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("建表失败（疑似损坏）: %w", err)
	}
	return db, nil
}

// Open 打开或创建历史库；损坏时删除重建（丢失历史可接受，绝不卡死功能）。
// 启动收尾：上次进程死于执行中的 planned 条目统一置 interrupted。
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := initConn(path)
	if err != nil {
		os.Remove(path)
		os.Remove(path + "-wal")
		os.Remove(path + "-shm")
		db, err = initConn(path)
		if err != nil {
			return nil, fmt.Errorf("历史库重建失败: %w", err)
		}
	}
	if _, err := db.Exec(`UPDATE op_items SET state = ?,
		err = '应用中断（记录不完整，未处理项请查看文件系统现状）'
		WHERE state = ?`, StateInterrupted, StatePlanned); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, path: path}, nil
}

// Close 释放句柄。
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Close()
}
