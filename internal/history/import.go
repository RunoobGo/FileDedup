// import.go —— 记录导入：把一份外来账本**增量**并进本地账本（M352，设计段 §6）。
//
// 用户裁定 R-2 的原话是「导入时以增量的形式扩展本地记录」，这句话里有三件事要同时成立，
// 每件都对应下面一段代码，缺任一件就退化成"恢复"或"翻倍"：
//
//	(1) 不删不改本地任何已有行（导入不是恢复）；
//	(2) 同一份文件导两次不翻倍 ⇒ 要有**自然键**判"这条本地已经有了"；
//	(3) 外来行的父子关联必须落到**本地**的 id 上（hist_groups.hist_id、
//	    hist_files.group_id、op_records.hist_id），照抄源库 id 就是错关联。
//
// ★ 与设计段 §6.2 的两处偏差（都被本轮实测否掉，读数见设计段 §12）：
//
//	一、**前置校验不许用 `history.Open(srcPath)`**。那条路上会执行
//	`PRAGMA journal_mode=WAL`、`PRAGMA user_version=N` 和两条 `UPDATE op_items`
//	（把残留 planned/undoing 收口），确证损坏时还会把文件**改名隔离**——全是改动
//	"用户刚在对话框里选中的那个文件"的动作。现读改为 `PRAGMA query_only=ON` 的只读连接：
//	打开前后字节一字未变（40960→40960），写语句被驱动拒回
//	`attempt to write a readonly database (8)`。
//
//	二、**合并不用 ATTACH**。实测事务内 `ATTACH DATABASE ? AS src` 可用（读到 1 行），
//	但同一事务里 `DETACH DATABASE src` 报 `database srcdb is locked (1)`——而设计段写的
//	顺序正是"ATTACH → 合并 → DETACH → COMMIT"。DETACH 只能留在事务外，那样一旦提交后
//	DETACH 失败，源库就挂在这条连接上（本库连接池恒 1），后续每条 SQL 都多一个可见的
//	`src.*` 命名空间 ⇒ 判据不再是"要么全成要么全不动"。改成：源侧只读连接逐表读、
//	本地单事务逐行写。逐行 INSERT 本就是本包既有形状（`SaveScan` 每个组、每个文件一条），
//	"避免 N+1 往返"的收益不在这条腿上；判重改成"进事务前把本地自然键一次读进内存"，
//	同样是一条集合式语句，且不依赖 ATTACH。
package history

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"filededup/internal/dbfile"
	"filededup/internal/sqlconn"

	"modernc.org/sqlite"
)

// ImportSummary 导入回执（M352）。五个数缺一不可，因为它们各挡一种"看不见的失败"：
// 只报 added 就说不清"这份文件我是不是已经导过了"（scansSkipped），
// 只报 scans 就说不清清理记录的关联有没有断（opsOrphaned）。
type ImportSummary struct {
	ScansAdded   int `json:"scansAdded"`
	ScansSkipped int `json:"scansSkipped"`
	OpsAdded     int `json:"opsAdded"`
	OpsSkipped   int `json:"opsSkipped"`
	// OpsOrphaned 是 hist_id 在源库里就指不到东西的清理记录条数（源库自己缺那行扫描）。
	// 它们的 hist_id 被置 0（= 无关联）并如实计数，不猜、不硬塞。
	OpsOrphaned int `json:"opsOrphaned"`
}

// importTableSpec 是一张外来表必须具备的名字与列（前置校验的判据）。
// 顺序固定（切片而非 map）：报错文案要能逐字复现，"缺哪张表"不该每次跑都不一样。
type importTableSpec struct {
	table   string
	columns []string
}

// importRequired 逐字取自 schemaSQL（history.go 的建表语句）。
// ★ 为什么把列名抄第二遍而不是"跑一条 SELECT 看报不报错"：撞运气式的校验只会
// 在缺列时给出 `no such column: hist_id` 这种英文驱动串，而这份判据存在的目的
// 正是"点名缺哪张表的哪一列"，让用户知道该重新导出还是该换一份文件。
var importRequired = []importTableSpec{
	{"scan_history", []string{"id", "saved_at", "roots", "filters", "threads", "paranoid",
		"groups_count", "files_count", "orig_files", "reclaimable", "failed_json", "keep_paths"}},
	{"hist_groups", []string{"id", "hist_id", "hash", "size", "reclaimable", "ord"}},
	{"hist_files", []string{"id", "hist_id", "group_id", "path", "size", "mtime_ns"}},
	{"op_records", []string{"id", "op_kind", "created_at", "target_dir", "hist_id", "undoable",
		"done_count", "reclaimed"}},
	{"op_items", []string{"id", "op_id", "orig_path", "dest_path", "link_src", "hash",
		"size", "mtime_ns", "state", "err"}},
}

// openReadOnly 以只读方式打开一份外来库（★ 不是 history.Open，理由见文件头偏差一）。
//
// query_only 是会话级 PRAGMA，必须逐连接重放（机制在 sqlconn，M59 在册），
// 且连接池收到 1：池里第 2 条连接若没吃到这条 PRAGMA，就是一条能写用户文件的腿。
var openReadOnly = func(path string) (*sql.DB, error) {
	base, err := sqlite.NewConnector(path)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(sqlconn.WithPragmas(base, []string{
		"PRAGMA query_only=ON",
		"PRAGMA busy_timeout=5000",
	}))
	db.SetMaxOpenConns(1)
	return db, nil
}

// validateForeignLedger 确认这份文件是一具**本包读得动**的账本：五张表都在、
// 每张表的必需列都在。在任何本地写入之前跑，认不过就一行都不动。
//
// 表名以**字符串常量**交给 pragma_table_info(?) 的参数位，不做语句拼接。
func validateForeignLedger(src *sql.DB) error {
	var missing []string
	for _, spec := range importRequired {
		rows, err := src.Query(`SELECT name FROM pragma_table_info(?)`, spec.table)
		if err != nil {
			return fmt.Errorf("读取外来记录的表结构失败（表 %s）: %w", spec.table, err)
		}
		have := make(map[string]bool, len(spec.columns))
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				rows.Close()
				return err
			}
			have[n] = true
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if len(have) == 0 {
			missing = append(missing, fmt.Sprintf("表 %s 整张不存在", spec.table))
			continue
		}
		var absent []string
		for _, c := range spec.columns {
			if !have[c] {
				absent = append(absent, c)
			}
		}
		if len(absent) > 0 {
			missing = append(missing, fmt.Sprintf("表 %s 缺列 %s", spec.table, strings.Join(absent, "/")))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("外来记录与本仓账本结构不符，导入已停止（未改动本地记录）：%s",
			strings.Join(missing, "；"))
	}
	return nil
}

// scanKey 与 opKey 是两条自然键（裁定 R-2 的判重依据）。
//
// ★ 为什么不能只比 saved_at / created_at：同一秒内的两次扫描（自动化、重试）会撞键，
// 判成"本地已有"就静默吞掉一条真记录——那是导入功能自己能造出来的数据丢失形状。
// ★ 为什么不认 id：id 是各库自己的 AUTOINCREMENT，跨库无意义，照 id 插要么撞主键、
// 要么错关联。
type scanKey struct {
	SavedAt     int64
	Roots       string
	Filters     string
	GroupsCount int
	FilesCount  int
}

type opKey struct {
	CreatedAt int64
	OpKind    string
	TargetDir string
	DoneCount int
	Reclaimed int64
}

// srcScan / srcOp 等是逐表搬运用的行形状，只在导入这条路上存在，
// 不进任何对外契约（对外的是 Mirror 与 ImportSummary）。
type srcScan struct {
	id                                int64
	savedAt                           int64
	roots, filters, failed, keepPaths string
	threads, groupsCount, filesCount  int64
	origFiles                         int64
	paranoid                          bool
	reclaimable                       int64
}

type srcGroup struct {
	srcID       int64
	ord         int
	hash        []byte
	size        int64
	reclaimable int64
}

type srcOp struct {
	id        int64
	kind      string
	targetDir string
	createdAt int64
	histID    int64
	doneCount int
	undoable  bool
	reclaimed int64
}

type srcItem struct {
	origPath, destPath, linkSrc string
	hash                        []byte
	size, mtimeNs               int64
	state, errMsg               string
}

func loadSrcScans(src *sql.DB) ([]srcScan, error) {
	rows, err := src.Query(`SELECT id, saved_at, roots, filters, threads, paranoid,
		groups_count, files_count, orig_files, reclaimable, failed_json, keep_paths
		FROM scan_history ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []srcScan
	for rows.Next() {
		var r srcScan
		var roots, filters, failed, keep []byte
		if err := rows.Scan(&r.id, &r.savedAt, &roots, &filters, &r.threads, &r.paranoid,
			&r.groupsCount, &r.filesCount, &r.origFiles, &r.reclaimable, &failed, &keep); err != nil {
			return nil, err
		}
		// NULL 列按空串搬运：这四列在本仓建表语句里都是 NOT NULL DEFAULT，只有**外来**文件
		// 才可能给 NULL；折成空串写回本地后，本地读侧（scanMeta）对空列的行为是"没有数据"，
		// 不是报错，也不会编出一个假的空数组。
		r.roots, r.filters, r.failed, r.keepPaths = string(roots), string(filters), string(failed), string(keep)
		out = append(out, r)
	}
	return out, rows.Err()
}

func loadSrcOps(src *sql.DB) ([]srcOp, error) {
	rows, err := src.Query(`SELECT id, op_kind, created_at, target_dir, hist_id, undoable,
		done_count, reclaimed FROM op_records ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []srcOp
	for rows.Next() {
		var (
			id            int64
			o             srcOp
			doneCount, rc int64
		)
		if err := rows.Scan(&id, &o.kind, &o.createdAt, &o.targetDir, &o.histID,
			&o.undoable, &doneCount, &rc); err != nil {
			return nil, err
		}
		o.id, o.doneCount, o.reclaimed = id, int(doneCount), rc
		out = append(out, o)
	}
	return out, rows.Err()
}

// ImportFrom 把 srcPath 那份账本增量并进本地。
//
// 顺序即判据：**存在性检查 → 只读打开 → 校验结构 → 读本地自然键 → 读外来行 →
// 开事务 → 逐父行插子行 → 提交**。事务里任何一步出错就 Rollback，本地零变化
// （含"子行插到第 37 行失败"这种半截情况——那正是设计段要求"要么全成要么全不动"要防的形状）。
//
// ★ 不做 MaxScanHistory 裁剪：裁剪要 DELETE 本地最旧的记录，直接违反约束 (1)，
// 而这个功能的全部理由就是"防止误删除"。代价如实说：导入后本地可能超过 20 条，
// 而**下一次扫描**的 SaveScan 会按 id 保留最新 20 条 ⇒ 刚导进来的记录若排在最旧一侧，
// 会在下一次扫描时被淘汰。这条交互写进手册（09/10），不藏。
func (s *Store) ImportFrom(srcPath string) (ImportSummary, error) {
	var sum ImportSummary
	if srcPath == "" {
		return sum, errors.New("导入路径为空")
	}
	if srcPath == s.path {
		return sum, fmt.Errorf("这份文件就是当前正在使用的账本（%s），无需导入", srcPath)
	}
	// 不存在必须在这里挡住：SQL 层对"文件不存在"会**建出**一个空库，于是校验报成
	// "五张表整张不存在"——一句话能说清的事被报成一串结构错误。
	if _, err := os.Stat(srcPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return sum, fmt.Errorf("找不到要导入的记录文件：%s", srcPath)
		}
		return sum, fmt.Errorf("读不到要导入的记录文件（%s，本地记录未改动）: %w", srcPath, err)
	}

	src, err := openReadOnly(srcPath)
	if err != nil {
		return sum, fmt.Errorf("打不开要导入的记录文件（本地记录未改动）: %w", err)
	}
	defer src.Close()

	if err := validateForeignLedger(src); err != nil {
		if dbfile.IsCorruption(err) {
			return sum, fmt.Errorf("要导入的记录文件不是一份可用的账本（本地记录未改动）：%v", err)
		}
		return sum, err
	}

	// 本地自然键先行读入：既是判重依据，也是"源里那条扫描被判成重复"时
	// op_records.hist_id 要指向的那个**本地 id**（约束 (3) 里最容易做错的一格）。
	localScans, err := s.localScanKeys()
	if err != nil {
		return sum, err
	}
	localOps, err := s.localOpKeys()
	if err != nil {
		return sum, err
	}
	scans, err := loadSrcScans(src)
	if err != nil {
		return sum, fmt.Errorf("读外来扫描历史失败（本地记录未改动）: %w", err)
	}
	ops, err := loadSrcOps(src)
	if err != nil {
		return sum, fmt.Errorf("读外来清理记录失败（本地记录未改动）: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return sum, err
	}
	defer tx.Rollback()

	// 源扫描 id → 本地扫描行 id。判成重复的也要进这张表（指向本地那一条），
	// 否则"扫描本地已有、清理记录是新来的"这一档会把 hist_id 错置成 0。
	scanMap := make(map[int64]int64, len(scans))
	for _, sc := range scans {
		k := scanKey{SavedAt: sc.savedAt, Roots: sc.roots, Filters: sc.filters,
			GroupsCount: int(sc.groupsCount), FilesCount: int(sc.filesCount)}
		if id, ok := localScans[k]; ok {
			sum.ScansSkipped++
			scanMap[sc.id] = id
			continue // 判重连子行一起不动 ⇒ 导两次不翻倍
		}
		histID, err := insertScanRow(tx, sc)
		if err != nil {
			return sum, err
		}
		localScans[k] = histID // 同一份文件里有两条自然键相同的扫描时，第二条要认成重复
		scanMap[sc.id] = histID
		sum.ScansAdded++
		if err := mergeScanChildren(tx, src, sc.id, histID); err != nil {
			return sum, err
		}
	}

	for _, op := range ops {
		k := opKey{CreatedAt: op.createdAt, OpKind: op.kind, TargetDir: op.targetDir,
			DoneCount: op.doneCount, Reclaimed: op.reclaimed}
		newHist := int64(0)
		if op.histID != 0 {
			local, ok := scanMap[op.histID]
			if !ok {
				// 源里这条 op 指向一个源库自己没有的扫描 ⇒ 无从映射，置 0 并如实记账。
				sum.OpsOrphaned++
			} else {
				newHist = local
			}
		}
		if id, ok := localOps[k]; ok {
			sum.OpsSkipped++
			_ = id // 本地已有 ⇒ 连子项一起不动（同上）
			continue
		}
		opID, err := insertOpRow(tx, op, newHist)
		if err != nil {
			return sum, err
		}
		localOps[k] = opID
		sum.OpsAdded++
		if err := mergeOpItems(tx, src, op.id, opID); err != nil {
			return sum, err
		}
	}
	return sum, tx.Commit()
}

// localScanKeys 一条集合式语句读回本地全部扫描自然键 → 行 id。
// 重复键（历史遗留或并发写造成）保留 id 最小的那条：判重只看存在性，
// 而"指到哪一条"要有一个不随查询计划变化的确定答案。
func (s *Store) localScanKeys() (map[scanKey]int64, error) {
	rows, err := s.db.Query(`SELECT id, saved_at, roots, filters, groups_count, files_count
		FROM scan_history ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[scanKey]int64)
	for rows.Next() {
		var (
			id                 int64
			k                  scanKey
			roots, filters     []byte
			groupsCount, files int64
		)
		if err := rows.Scan(&id, &k.SavedAt, &roots, &filters, &groupsCount, &files); err != nil {
			return nil, err
		}
		k.Roots, k.Filters = string(roots), string(filters)
		k.GroupsCount, k.FilesCount = int(groupsCount), int(files)
		if _, dup := out[k]; !dup {
			out[k] = id
		}
	}
	return out, rows.Err()
}

func (s *Store) localOpKeys() (map[opKey]int64, error) {
	rows, err := s.db.Query(`SELECT id, created_at, op_kind, target_dir, done_count, reclaimed
		FROM op_records ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[opKey]int64)
	for rows.Next() {
		var (
			id                 int64
			k                  opKey
			doneCount, reclaim int64
		)
		if err := rows.Scan(&id, &k.CreatedAt, &k.OpKind, &k.TargetDir, &doneCount, &reclaim); err != nil {
			return nil, err
		}
		k.DoneCount, k.Reclaimed = int(doneCount), reclaim
		if _, dup := out[k]; !dup {
			out[k] = id
		}
	}
	return out, rows.Err()
}

func insertScanRow(tx *sql.Tx, r srcScan) (int64, error) {
	res, err := tx.Exec(`INSERT INTO scan_history
		(saved_at, roots, filters, threads, paranoid,
		 groups_count, files_count, orig_files, reclaimable, failed_json, keep_paths)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.savedAt, r.roots, r.filters, r.threads, r.paranoid,
		r.groupsCount, r.filesCount, r.origFiles, r.reclaimable, emptyJSON(r.failed), emptyJSON(r.keepPaths))
	if err != nil {
		return 0, fmt.Errorf("写入外来扫描历史失败（本地记录未改动）: %w", err)
	}
	return res.LastInsertId()
}

func insertOpRow(tx *sql.Tx, r srcOp, histID int64) (int64, error) {
	res, err := tx.Exec(`INSERT INTO op_records
		(op_kind, created_at, target_dir, hist_id, undoable, done_count, reclaimed)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.kind, r.createdAt, r.targetDir, histID, r.undoable, r.doneCount, r.reclaimed)
	if err != nil {
		return 0, fmt.Errorf("写入外来清理记录失败（本地记录未改动）: %w", err)
	}
	return res.LastInsertId()
}

// mergeScanChildren 把一个源扫描的组与文件搬到本地新行下。
//
// 源侧只读取一条组列表、关掉，再逐组取文件——任何时刻内存里躺着的是一组文件，
// 不是整本账（外来文件可能有十几万行 hist_files）。
// ★ hist_files.group_id 必须换成**新组 id**：照抄源库 group_id 会让文件挂到
// 本地别人的组上（错关联，且列表看起来一切正常，只有展开内容时才露馅）。
func mergeScanChildren(tx *sql.Tx, src *sql.DB, srcHistID, newHistID int64) error {
	rows, err := src.Query(`SELECT id, hash, size, reclaimable, ord FROM hist_groups
		WHERE hist_id = ? ORDER BY ord ASC, id ASC`, srcHistID)
	if err != nil {
		return err
	}
	var groups []srcGroup
	for rows.Next() {
		var g srcGroup
		if err := rows.Scan(&g.srcID, &g.hash, &g.size, &g.reclaimable, &g.ord); err != nil {
			rows.Close()
			return err
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, gr := range groups {
		res, err := tx.Exec(`INSERT INTO hist_groups (hist_id, hash, size, reclaimable, ord)
			VALUES (?, ?, ?, ?, ?)`, newHistID, gr.hash, gr.size, gr.reclaimable, gr.ord)
		if err != nil {
			return fmt.Errorf("写入外来重复组失败（本地记录未改动）: %w", err)
		}
		newGroupID, err := res.LastInsertId()
		if err != nil {
			return err
		}
		fs, err := src.Query(`SELECT path, size, mtime_ns FROM hist_files
			WHERE group_id = ? ORDER BY id ASC`, gr.srcID)
		if err != nil {
			return err
		}
		for fs.Next() {
			var path string
			var size, mtimeNs int64
			if err := fs.Scan(&path, &size, &mtimeNs); err != nil {
				fs.Close()
				return err
			}
			if _, err := tx.Exec(`INSERT INTO hist_files (hist_id, group_id, path, size, mtime_ns)
				VALUES (?, ?, ?, ?, ?)`, newHistID, newGroupID, path, size, mtimeNs); err != nil {
				fs.Close()
				return fmt.Errorf("写入外来重复文件行失败（本地记录未改动）: %w", err)
			}
		}
		if err := fs.Err(); err != nil {
			fs.Close()
			return err
		}
		fs.Close()
	}
	return nil
}

func mergeOpItems(tx *sql.Tx, src *sql.DB, srcOpID, newOpID int64) error {
	rows, err := src.Query(`SELECT orig_path, dest_path, link_src, hash, size, mtime_ns, state, err
		FROM op_items WHERE op_id = ? ORDER BY id ASC`, srcOpID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var it srcItem
		if err := rows.Scan(&it.origPath, &it.destPath, &it.linkSrc, &it.hash,
			&it.size, &it.mtimeNs, &it.state, &it.errMsg); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO op_items
			(op_id, orig_path, dest_path, link_src, hash, size, mtime_ns, state, err)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			newOpID, it.origPath, it.destPath, it.linkSrc, it.hash, it.size, it.mtimeNs,
			it.state, it.errMsg); err != nil {
			return fmt.Errorf("写入外来清理条目失败（本地记录未改动）: %w", err)
		}
	}
	return rows.Err()
}

// emptyJSON 给两个 JSON 文本列兜一个 '[]'：外来文件里这两列给 NULL 或空串时，
// 本地读侧（scanMeta）把空列解成"没有数据"，但下一句 UPDATE 拼 JSON 时留空更容易出错，
// 按建表默认值补齐。failed_json 与 keep_paths 的默认值都是 '[]'，故共用一条规则。
func emptyJSON(v string) string {
	if strings.TrimSpace(v) == "" {
		return "[]"
	}
	return v
}
