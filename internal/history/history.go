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

	"filededup/internal/dbfile"
	"filededup/internal/sqlconn"

	"modernc.org/sqlite"
)

// MaxScanHistory 扫描历史上限，超出淘汰最旧（CASCADE 清组/文件行）。
const MaxScanHistory = 20

// SchemaVersion 写进 PRAGMA user_version 的库结构版本号。
//
// M97 如实限定：**目前只写不读** —— Open 全路径没有一处读回比对，因此它
// **不构成版本门禁**，改结构时旧库不会被自动作废或迁移（对照 cache 侧的
// enforceAlgoVersion，那才是真门禁：不符即整表作废）。
// 补真门禁属改行为，本批不做；在此之前不得把这个常量读成"有版本校验"。
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
	// StateUndoing 写前标记：已判定要回撤、文件系统动作尚未收口。
	// 只在执行的瞬间存在，进程死亡后由 Open 收口为 undo_failed（I6）。
	StateUndoing = "undoing"
)

// Store 线程安全历史库（单写多读；全部方法内部串行化）。
type Store struct {
	mu   sync.Mutex
	db   *sql.DB
	path string
	// quarantined 非空表示本次 Open 走了"影像损坏→改名隔离→重建"，值是隔离后的文件名。
	// 只在 Open 里写一次，之后只读（见 QuarantinedTo）。
	quarantined string
}

// QuarantinedTo 返回本次打开时被隔离的旧库文件名；未发生隔离则返回空串。
// M12b（2026-09-21 全仓审计 §五 12）：隔离重建后用户看到的是"历史记录页凭空变空"，
// 而原因只写在 stderr——GUI 用户根本没有终端。这个出口让 App 层能把原因说给界面。
func (s *Store) QuarantinedTo() string { return s.quarantined }

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

// connPragmas 是账本库的**会话级** PRAGMA，逐连接重放（机制见 internal/sqlconn）。
//
// M59（04 §6.11 APP-11，实测读数见设计段 §19.0-1）：改前这三条只在开池时 db.Exec
// 一遍，而池的连接一旦被回收（空闲超时 / SetConnMaxLifetime 到期 / ErrBadConn），
// 新建的连接就读回 SQLite 默认值 —— database/sql 不会重放任何会话级语句。
// 本机实测：foreign_keys 1→0、busy_timeout 5000→0；synchronous 恰好默认就是 FULL，
// 这一条侥幸无损，所以划账时不得把三条一并说成"全部失效"。
// foreign_keys 归零不是"约束没了"那么轻：本库四处 ON DELETE CASCADE 静默失效，
// 删扫描历史只删主表、子表留孤儿行，LoadScan 还能按旧 hist_id 捞出残留组。
var connPragmas = []string{
	// M11（2026-09-21 全仓审计 §五 11）：账本必须 FULL，**不与 cache 同口径**。
	// NORMAL 下 commit 只写 WAL 不 fsync，掉电/内核崩溃时最近的 BeginOp/FinishItem
	// 随 WAL 一起丢——而**删除动作已经生效**，账本丢了就等于永久失去撤销依据。
	// （进程崩溃不受影响，WAL 会回放；这里防的是断电那一档。）
	// cache 是可再生件（丢了重算即可），同一参数对它成立、对账本不成立。
	// 代价实测可忽略（本机 APFS，2026-09-21）：500 条 FinishItem 逐条提交
	// NORMAL 64.9ms → FULL 79.6ms，约 30µs/条，而每条对应的文件操作本身就要
	// 一次落盘；全仓 5 个基准无一涉及账本写入路径，不存在以 NORMAL 为前提的性能结论。
	"PRAGMA synchronous=FULL",
	"PRAGMA foreign_keys=ON",
	// 跨进程（fdd-cli 与 GUI 并存）瞬时占用时让路 5s，别把 BUSY 报成故障。
	"PRAGMA busy_timeout=5000",
}

// initConn 打开账本库。
//
// 连接池限 1 的理由（与 M59 无关，是另一件事）：本库写入频率极低（扫描/清理各一次
// 批量），单连接从根上消除 SQLITE_BUSY 面。M59 之前它还被当成"会话级 PRAGMA 必然
// 生效"的依据，那个依据已在连接被回收时证伪，故收归到 connPragmas 逐连接重放；
// 缩池本身保留（§19.6-7：不做"把池改大"，避免把 M73 的实测代价搬到账本上瞎猜）。
func initConn(path string) (*sql.DB, error) {
	base, err := sqlite.NewConnector(path)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(sqlconn.WithPragmas(base, connPragmas))
	db.SetMaxOpenConns(1)
	// 只留**文件级**的两条：journal_mode=WAL 是持久属性（实测新连接直接读到 wal），
	// user_version 是库头里的整数，二者都不需要逐连接重放。
	for _, p := range []string{
		"PRAGMA journal_mode=WAL",
		fmt.Sprintf("PRAGMA user_version=%d", SchemaVersion),
	} {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("初始化失败: %w", err)
		}
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("建表失败: %w", err)
	}
	return db, nil
}

// Open 打开或创建历史库。**仅**在库影像确证损坏时改名隔离并重建
// （2026-09-18 审查 C4：原先对任何打开错误都无条件删库，busy/只读/满盘一次误判
// 就会清空用户全部回撤账本）。启动收尾：上次进程死于执行中的 planned
// 条目统一置 interrupted，死于回撤中的 undoing 条目统一置 undo_failed（可重试）。
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := initConn(path)
	var quarantinedName string
	if err != nil {
		if !dbfile.Exists(path) || !dbfile.IsCorruption(err) {
			// 暂时性/环境故障：报出去（调用方会失去历史能力但绝不丢数据）。
			return nil, fmt.Errorf("历史库不可用（未改动任何文件）: %w", err)
		}
		quarantined, qerr := dbfile.Quarantine(path)
		if qerr != nil {
			return nil, fmt.Errorf("历史库影像损坏但隔离失败，已放弃重建（未删除文件）: %w（原始错误：%v）", qerr, err)
		}
		fmt.Fprintf(os.Stderr, "[history] 历史库影像损坏，已改名隔离为 %s 后重建\n", quarantined)
		quarantinedName = quarantined // M12b：还要经 QuarantinedTo 说给界面，见下面的 accessor
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
	// 回撤写前标记残留（I6）：上次进程死于「已标记回撤、结果未落账」之间。
	// 不猜测文件系统现状——置 undo_failed 并说明原因，重试会由 UndoOne 的
	// 已还原自检决定真实结局（文件已回家则记成功，否则如实报错）。
	if _, err := db.Exec(`UPDATE op_items SET state = ?,
		err = '回撤中断（结果未落账）：请重试回撤确认文件系统现状'
		WHERE state = ?`, StateUndoFailed, StateUndoing); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, path: path, quarantined: quarantinedName}, nil
}

// Close 释放句柄。
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Close()
}
