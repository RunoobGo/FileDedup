// Package cache 哈希缓存（04 M4-T01，01 §7.3）：
// SQLite WAL；键 (path, size, mtime_ns, dev, ino, ctime_ns)；
// 命中后仍须四点采样比对，全一致才复用 full；
// 批量 UPSERT 单事务写回；last_hit 上限淘汰。
// 损坏处理分两层，都不是"自愈"（M96）：**打开时**确证影像损坏 ⇒ 改名隔离后重建；
// **运行期**确证损坏 ⇒ 只停用（不再发 SQL），要恢复必须重启应用（M87 未做）。
package cache

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"filededup/internal/dbfile"
	"filededup/internal/fsid"
	"filededup/internal/sqlconn"

	"modernc.org/sqlite"
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
	Dev     uint64 // 物理身份的卷号：unix 取 fstat 的 st_dev（fsid_unix.go:16），Windows 取句柄查询的卷序列号（fsid_windows.go:201，I7 起**不再恒 0**）
	Ino     uint64 // 同上腿的 inode / 文件索引
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

	// M74：运行期库错误的证据位。改前 Lookup 把一切 Scan 错误折成"未命中"，
	// 于是"库中途坏了"这件事只有 Store 每轮报一次错、Lookup 永远沉默，
	// 且没有任何地方再判损坏（IsCorruption 只在 Open 跑过一次）。
	dbErrs  atomic.Int64 // Lookup 遇到的非 ErrNoRows DB 错误条数（进程启动以来累计，M95）
	corrupt atomic.Bool  // 确证损坏 ⇒ 不再向库发 SQL（停用，不是自愈）
}

// DBErrors 返回 Lookup 累计到的运行期 DB 错误条数（M74；不含正常的"未命中"）。
// 口径与上面的字段一致：**进程启动以来**的累计，不随扫描轮次归零（M95）——
// 全仓只有 Add 与 Load 两个访问点，没有任何重置点，所以"本轮"这个说法是假的。
func (c *Cache) DBErrors() int64 { return c.dbErrs.Load() }

// Corrupted 返回本库是否已被确证损坏并停用（M74）。
// 措辞约束：置位只代表"不再用它"，不代表"已隔离/已重建"——那一步没有做（§18.6 → M87）。
func (c *Cache) Corrupted() bool { return c.corrupt.Load() }

// ErrCorruptDisabled 库被确证损坏后的自我停用（M74）。句子是完整的，调用方原样上报即可。
var ErrCorruptDisabled = errors.New("哈希缓存库在运行期确证损坏，已停用：不影响去重结果，只是每次扫描都要重算")

// ErrEvictFailed 写回已 Commit 成功、只有 LRU 淘汰失败（M75(a)）。
// 上游必须据此分岔文案——把它报成"缓存写回失败"是一句假话（条目已经在库里）。
var ErrEvictFailed = errors.New("缓存 LRU 淘汰失败")

// noteDBError 记一次运行期库错误（M74）：计数 + 确证损坏时置停用位。
// 计数用原子、不持 c.mu：Lookup 的读侧持 RLock，这里若在持锁路径上再取写锁会自锁。
func (c *Cache) noteDBError(err error) {
	c.dbErrs.Add(1)
	c.markCorruption(err)
}

// markCorruption 只在 SQLite 原文确指"库本身损坏"时停用；BUSY/只读/满盘等暂时性
// 故障不置位（判据与 Open 那条同源于 dbfile.IsCorruption，不另起一套）。
func (c *Cache) markCorruption(err error) {
	if dbfile.IsCorruption(err) {
		c.corrupt.Store(true)
	}
}

// Open 打开或创建缓存库。
//
// **仅**在库影像确证损坏时改名隔离并重建（最坏退化为首扫速度）；
// busy/只读/满盘等暂时性故障一律直接报错（2026-09-18 审查 C4：原先无条件删库重建，
// 一次误判就把可恢复的暂时故障变成整表缓存丢失，且删掉他进程正在写的
// -wal 本身即可损坏主库）。调用方拿到错误会退化为「无缓存」，扫描照常。
func Open(path string) (*Cache, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := openDB(path)
	if err != nil {
		if !dbfile.Exists(path) || !dbfile.IsCorruption(err) {
			return nil, fmt.Errorf("缓存库不可用（未改动任何文件）: %w", err)
		}
		quarantined, qerr := dbfile.Quarantine(path)
		if qerr != nil {
			return nil, fmt.Errorf("缓存库影像损坏但隔离失败，已放弃重建（未删除文件）: %w（原始错误：%v）", qerr, err)
		}
		fmt.Fprintf(os.Stderr, "[cache] 缓存库影像损坏，已改名隔离为 %s 后重建\n", quarantined)
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

// connPragmas 是**每条连接**都要执行的会话级 PRAGMA。
//
//	busy_timeout=5000  跨进程（fdd-cli 与 GUI 并存）瞬时占用时让路 5s，别把 BUSY 报成故障。
//	synchronous=NORMAL 缓存是可再生件，NORMAL 足够；账本库刻意用 FULL，两库口径不同
//	                   是裁定不是疏漏（见 history.go 的 M11 注释）。
//
// journal_mode=WAL 不在这里：它是文件级持久属性，建库时执行一次即可（实测新连接直接读到 wal）。
//
// 逐连接重放的**机制**在 internal/sqlconn（M59 收归，I5）；这里留的是本库的**策略**：
// 两个库的 synchronous 取值本来就不同（缓存 NORMAL / 账本 FULL），把清单也搬进
// sqlconn 就成了"一份代码写死两个库的口径"，那是假收归。
var connPragmas = []string{"PRAGMA busy_timeout=5000", "PRAGMA synchronous=NORMAL"}

// 为什么不用登记原文建议的两种修法（04 §6.11 M73，实测读数见设计段 §18.0 取证 #3/#4）：
//   - DSN URI 形（`file:...?_pragma=`）：含 `#` 的路径会被当成 URI fragment，实测库
//     建到了 `…/a b` 而不是 `…/a b#c中文/cache.db`；Windows 路径经 url.URL 还会把
//     `C:` 吃成 authority。缓存悄悄建到别处 = 每轮白算且永不自愈。
//   - SetMaxOpenConns(1)（照抄 history）：实测 8 worker × 3000 次主键点查
//     139.1ms → 289.8ms（2.08 倍），本包"Lookup 用 RLock 并行"是承重的。
//
// 走 Connector 包装还有一条附带好处：路径始终以**纯文件名**交给驱动，与改前同形，
// 因此不存在"新平台 URI 解析差异"这种无法在本机验证的面。包装本体见 sqlconn.WithPragmas。
func openDB(path string) (*sql.DB, error) {
	base, err := sqlite.NewConnector(path)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(sqlconn.WithPragmas(base, connPragmas))
	// 文件级 PRAGMA 与表结构（01 §7.3）；会话级的两条见 connPragmas（M73）。
	for _, p := range []string{
		"PRAGMA journal_mode=WAL",
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
			// 不写「疑似损坏」：BUSY/只读/满盘走同一分支，措辞不能替用户下结论
			// （是否损坏由 dbfile.IsCorruption 看 SQLite 原文判定）
			return nil, fmt.Errorf("初始化失败: %w", err)
		}
	}
	return db, nil
}

// Lookup 命中判定：path 存在且 size/mtime 与物理身份 (dev, ino, ctime_ns)
// 完全一致。id 未解析时身份比较平凡通过（这一格与平台无关，见下方 F1④ 说明），
// 兜底仍靠四点采样。
// 返回条目与 full 是否有效（决定阶段 3 是否跳过）。
func (c *Cache) Lookup(path string, size uint64, mtimeNs int64, id fsid.ID) (Entry, bool, bool) {
	if c.corrupt.Load() {
		return Entry{}, false, false // M74：已确证损坏 ⇒ 不再发 SQL（当次未命中，重算即可）
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	row := c.db.QueryRow(
		`SELECT size, mtime_ns, partial, full, dev, ino, ctime_ns FROM hash_cache WHERE path = ?`, path)
	var e Entry
	var partial []byte
	var dev64, ino64 int64
	e.Path = path
	if err := row.Scan(&e.Size, &e.MtimeNs, &partial, &e.Full, &dev64, &ino64, &e.CtimeNs); err != nil {
		// M74：改前这里一律折成"未命中"，于是"库坏了"与"这条没有"在证据上完全等价，
		// 而损坏判定只在 Open 跑过一次 ⇒ 库中途坏了永不自愈，Lookup 侧全程沉默。
		// 未命中仍然是正确的兜底行为（宁可重算），但必须留下证据、且确证损坏时停用。
		if !errors.Is(err, sql.ErrNoRows) {
			c.noteDBError(err)
		}
		return Entry{}, false, false
	}
	e.Dev, e.Ino = uint64(dev64), uint64(ino64)
	if e.Size != size || e.MtimeNs != mtimeNs {
		return Entry{}, false, false // 元数据变化 → 未命中（重算）
	}
	// H1：mtime 无法证明内容未变（粗粒度卷/原地改写保 mtime），ctime 与
	// inode 身份提供第二重证据：写内容、rename、chmod 都会推进 ctime；
	// 路径被换成另一文件则 dev/ino 必不同。不一致宁可当次未命中重算。
	// ★ F1④：跳过这一道比较的条件是 `!id.Resolved`（身份没解析出来），**与平台无关**——
	//   Windows 自 fsid I7 起走句柄查询，正常也有卷号+索引；只有查询失败或卷不提供
	//   稳定索引（FAT/exFAT 等，见 fsid_windows.go:191 的 index==0 判定）才落到这一格。
	//   旧注释把这一格写成 Windows 专属（「该值恒为 0、比较平凡通过」）——那是 I7 之前的
	//   事实，别再照它推理。
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
func (c *Cache) Store(entries []Entry) (err error) {
	if len(entries) == 0 {
		return nil
	}
	if c.corrupt.Load() {
		return ErrCorruptDisabled // M74：停用后不再发 SQL，也不每轮重犯
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// M74：Store/Touch 的错误本来就会上报给用户，这里只补"确证损坏 ⇒ 停用"这一半。
	defer func() {
		if err != nil {
			c.markCorruption(err)
		}
	}()
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
	// M75(a)：走到这里**哈希已经落库**，淘汰失败是另一件事。改前这里直接
	// `return c.evictLocked()`，上游把两种失败一律写成"缓存写回失败"（假话）。
	if e := evictFn(c); e != nil {
		return fmt.Errorf("%w：哈希条目已写回，仅 LRU 淘汰未完成（%v）", ErrEvictFailed, e)
	}
	return nil
}

// Touch 批量刷新命中条目的 last_hit（LRU 语义：命中即续期）。
// R1 修复：此前命中条目永不续期，高频命中的热文件反而最先被淘汰。
// 单事务执行；空列表无操作。
func (c *Cache) Touch(paths []string) (err error) {
	if len(paths) == 0 {
		return nil
	}
	if c.corrupt.Load() {
		return ErrCorruptDisabled // M74：同上，停用后不再发 SQL
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	defer func() {
		if err != nil {
			c.markCorruption(err)
		}
	}()
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

// evictFn 是淘汰步骤的注入点（测试接缝，惯例同 ops.verifyFileFn / scanner.probeCaseVerdict）。
// 生产恒等于 evictLocked。为什么要接缝：Store 在 Commit 之后会把 cntValid 置回 false，
// evictLocked 于是先重算 COUNT、只有真超限才发 DELETE —— 想稳定造出"已写回、淘汰失败"
// 这一档，就得往库里塞 50 万条（MaxEntries），或留一个把计数伪造成超限的口子；
// 两者都比一条接缝贵。"DELETE 确实会失败"这件事另有独立自检（见 cache_m73_m75_test.go
// 的触发器用例），不靠这条接缝自证。
var evictFn = func(c *Cache) error { return c.evictLocked() }

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
