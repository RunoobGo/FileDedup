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
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"filededup/internal/dbfile"
	"filededup/internal/pathnorm"
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
	// ★ M371：只计**真的落库**的行——被去重跳过的重复导入不在这里记账，
	// 否则回执会出现"没新增任何东西，却发现 N 条孤儿"这种自相矛盾。
	OpsOrphaned int `json:"opsOrphaned"`
	// OpsUndoDowngraded 是 DDP-9 那一闸降级的笔数：外来可回撤记录里，
	// 落点不在本机任何扫描根之下（或不在任何已知路径清单里）的那几笔，
	// 导入时已被标成 **undoable=0**，即在本机没有任何入口能发起回撤。
	// 记在回执里是因为"导入了 5 笔、有 2 笔不能回撤"必须看得见，
	// 否则用户会以为回撤按钮坏了。
	OpsUndoDowngraded int `json:"opsUndoDowngraded"`
	// OpsUndoDowngradeNote 是那句降级说明（为空即没有发生降级）。非空时前端应当显示。
	OpsUndoDowngradeNote string `json:"opsUndoDowngradeNote"`
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

// undoEvidenceLen 是内容证据的满长字节数，取自回撤读腿实际用的那个数组类型
// （OpItem.Hash 是 [32]byte），不写死 32：将来证据换形时这一格要跟着判据一起改，
// 而不是留一个还绿着的字面量。
const undoEvidenceLen = len([32]byte{})

// undoEvidenceSQL 问的是"**回撤真会吃到的那一集合**"里有没有条目拿不出满长内容证据。
//
// 为什么这一格必须在导入侧判死：ops/undo.go 的 undoSourceCheck 与 restoreInPlace 在
// `it.Hash == [32]byte{}` 时会**同时**跳过全量 BLAKE3 复核与身份复核，只剩一条按
// `it.Size` 的比对——而 size 是外来账本自己填的。于是"来历不明的 .db + 点一次全部回撤"
// = 按外来路径搬动本机真实文件，且账本记"回撤成功"。前置校验 importRequired 只看
// **列名在不在**、不看值，所以这条豁免今天能被外来文件选中（真读数见 §6.67 的 P-7）。
//
// 三处判据细节，每一处都对应本轮实测到的一个形状：
//   - COALESCE(length(i.hash), -1) <> ?：SQLite 三值逻辑下 length(NULL) <> 32 判定成
//     NULL（不是真），只写长度比较会被 NULL 绕过 ⇒ 空值必须先折成一个必不等于满长的数。
//   - i.hash = 满长全零：这一支是设计段 §1.2 原判据**漏掉**的一档，实施时补上（见 §1.6 追记）。
//     只量长度的闸会放行它，而它经 oplog.go 的 copy(it.Hash[:], hb) 折出来的正是那个零值数组
//     ⇒ 同一个洞，只是换了个字节数。
//   - state IN (done, undo_failed)：与 app_ops.go 的回撤选择集**逐字同一集合**
//     （if it.State == history.StateDone || it.State == history.StateUndoFailed）。
//     放宽到全表就是过度拒绝——会把"只搬历史台账看数据"这一档正常用法打掉（P-4/P-5 钉它）。
//
// ★ 满长零值 BLOB 走参数位绑定，不做语句拼接。
const undoEvidenceSQL = `SELECT COUNT(*), COUNT(DISTINCT i.op_id) FROM op_items i
	JOIN op_records r ON r.id = i.op_id
	WHERE r.undoable = 1 AND i.state IN (?, ?)
	  AND (COALESCE(length(i.hash), -1) <> ? OR i.hash = ?)`

// checkUndoEvidence 在**任何本地写入之前**拒绝"外来账本给回撤豁免开后门"。
// 取向出自裁定 R-8-1（只在导入侧 fail-closed、不加列、不改回撤腿的零值放行分支）：
// 本地老账本里本就存在的零哈希行行为一字不动，只是这条豁免**不再能由外来文件选中**。
//
// 代价如实说（要写进 09/10，不藏）：用户自己的"升级前老账本 → 新版导出 → 导进另一台机"
// 会被这一闸挡住，那本老账的条目拿不出内容证据。
func checkUndoEvidence(src *sql.DB) error {
	var badItems, badOps int
	if err := src.QueryRow(undoEvidenceSQL,
		StateDone, StateUndoFailed, undoEvidenceLen, make([]byte, undoEvidenceLen)).
		Scan(&badItems, &badOps); err != nil {
		return fmt.Errorf("读外来账本的内容证据失败（本地记录未改动）: %w", err)
	}
	if badItems == 0 {
		return nil
	}
	return fmt.Errorf("外来记录里有 %d 条可回撤的清理条目（涉及 %d 笔操作）拿不出满 %d 字节的内容证据；"+
		"这种条目一旦导入，回撤会跳过内容比对与身份复核，可能按外来路径搬动本机文件，"+
		"因此本次导入已拒绝，本地记录未改动。请在本机重新扫描并清理后再导出，或只导入新版导出的记录",
		badItems, badOps, undoEvidenceLen)
}

// ─── DDP-9：路径侧的同一道闸 ───
//
// checkUndoEvidence 封的是**内容证据**那一半，DDP-9 封的是**路径**那一半。两者缺一，
// 威胁模型就只完成一半：补上 hash 之后，攻击者仍可填 `orig_path` / `dest_path` /
// `size` / `state` 四个字段，让"回撤"变成"把本机 P 处那份文件 rename 到任意外部 Q，
// 并在 Q 之下 MkdirAll 出任意目录树"。
//
// 为什么这两半必须成对：`internal/ops` 侧对 OrigPath **零校验**——
// undoSourceCheck（undo.go:91）只 Lstat/IsRegular/size/BLAKE3 校验 **DestPath**，
// restoreInPlace（undo.go:160/209）拿 OrigPath 直接 MkdirAll + renameFile。
// 也就是说 DestPath 的校验拦的是"搬错文件"，**没有一条判据管"搬到哪里去"**。
//
// ★ 取向与 M355 同源（裁定 R-8-1 的"只在导入侧 fail-closed"）：这条闸也只落导入侧，
//   不加列、不改回撤腿、不动本地老账本的零值放行分支。差别只在"合法来源"的定义：
//   M355 认「满长非零内容证据」，本闸认「路径落在本机某次扫描的根下」——
//   后者才是"这份记录说的是本机的文件"这句话在**路径**上的对应物。
//
// ★ 为什么不用「精确同值」那条更保守的取向：用户把 A 机的账本导到 B 机是**这个功能的
//   正当用途**（手册 09 §6.8「只导入自己导出过的文件」讲的是"别导来路不明的"，
//   不是"只许导回本机"）。而两机的用户目录布局通常相同 ⇒ 根前缀对得上而完整路径逐字
//   不同。精确同值会把这一档正常用法打死，那与"用全表拦截换一条绿"是同一种假修法
//   （M355 的注释里点名过这个形状）。
//
// ★ 一处刻意的**不拦**：本机一个扫描都没有时（全新安装、刚导入第一份账本），
//   根集合是空的 ⇒ 外来可回撤条目一律拒。理由是这时"本机扫描根"这个概念还不存在，
//   而放行等于把闸整个关掉。代价如实说：那台机器上第一份外来账本导不进可回撤条目，
//   用户先扫一次盘即可。这与手册 09 §6.8 已有的那句代价同向、可叠加。

// foreignUndoPathSQL 问的是与 undoEvidenceSQL **同一个集合**里每条条目的 op_id 与 orig_path。
// 只取 orig_path：DestPath 在回撤侧已被 undoSourceCheck 全量校验（存在 + 常规文件 +
// size + BLAKE3），而 OrigPath 一路到 renameFile 都没人看。
const foreignUndoPathSQL = `SELECT i.op_id, i.orig_path
	FROM op_items i
	JOIN op_records r ON r.id = i.op_id
	WHERE r.undoable = 1 AND i.state IN (?, ?)`

// localScanRootsSQL 取本机全部扫描根。判据取"任一根下即放行"而不是"所有根下"：
// 前者是"这条路径有没有可能来自本机"，后者是"必须同时属于每一次扫描"，那会拦掉
// 同一台机器上先后扫过不同目录的正常账本。
const localScanRootsSQL = `SELECT roots FROM scan_history`

// foreignPathSep 是外来账本路径的归一分隔符：取**字面** "\\" 而不是
// string(filepath.Separator)——这里处理的是**账本里存下来的字符串**，
// 那份账本可能是在 Windows 上写的，而 Windows 的 `\` 在 POSIX 上是合法文件名字符。
// 与 sysguard 的跨平台保护清单同款（pathnorm 包注释里点名的第二种合法给法）。
const foreignPathSep = "\\"

// markForeignUndoPaths 把"落点越界"的外来可回撤记录在本机**标成不可撤**，
// 返回被标了多少笔（外加一句给界面看的说明，非空即发生了降级）。
//
// ★ 为什么是"降级"而不是"整单拒绝"——这一格改过一次取向，值得把理由写全：
//
//	第一版是整单拒绝。实测打脸：既有夹具（TestM355ImportAcceptsItemWithFullContentEvidence
//	等 7 条）造的是「本地根 /mine/a + 外来根 /theirs/c」，模拟的正是**跨机导入**，
//	而两机布局不同是这一档的常态。整单拒绝把"把 A 机的账本导到 B 机看"这个功能的
//	正当用途整条打死 ⇒ 那就是 M355 注释里点名的"用一刀拦截换一条绿"，
//	而且是与 M355 自己立的"负控制必须能单独红"纪律同性质的假修法。
//
//	降级取向为什么足够：威胁是"**回撤**能按外来路径搬本机文件"。
//	把 undoable 标成 0 之后，这条记录在本机就**没有任何入口**能发起回撤——
//	不是"多点一次会失败"，是记录页不摆「回撤」按钮、批量回撤不收它、
//	GetOp 读出的 Undoable 为 false 直接回绝（app_ops.go:331）。
//	能力被摘掉，威胁随之消失，而"看记录"这一档一点没少。
//
//	这也是 R-8-1 那句"不加列、不改回撤腿"的延伸：不加列（复用已有的
//	op_records.undoable），不改回撤腿（一行代码都不动），只改**导入时写进去的值**。
//	本地老账本的 undoable=1 一条不动——那些是本机自己写的，落点天然有本机来源。
func markForeignUndoPaths(src, local *sql.DB) (badOps map[int64]bool, note string, err error) {
	roots, err := localScanRoots(local)
	if err != nil {
		return nil, "", err
	}
	known, err := localKnownPaths(local)
	if err != nil {
		return nil, "", err
	}
	rows, err := src.Query(foreignUndoPathSQL, StateDone, StateUndoFailed)
	if err != nil {
		return nil, "", fmt.Errorf("读外来账本的回撤落点失败（本地记录未改动）: %w", err)
	}
	defer rows.Close()

	// 越界的**源侧 op_id 集合**。降级作用在 op_records.undoable 上（那是回撤唯一读的列），
	// 所以按 op 归并而不是按 item：一个 op 里 100 个条目只要有 1 个越界，
	// 那一笔就不能整笔回撤——回撤是整笔触发的，留一条缝就等于没封。
	badOps = make(map[int64]bool)
	var sample []string
	seen := make(map[string]bool)
	for rows.Next() {
		var opID int64
		var p string
		if err := rows.Scan(&opID, &p); err != nil {
			return nil, "", fmt.Errorf("读外来账本的回撤落点失败（本地记录未改动）: %w", err)
		}
		if p != "" && foreignPathAllowed(p, roots, known) {
			continue
		}
		badOps[opID] = true
		// 同一个越界路径在账本里出现几百次（一次清理动了几百个文件），
		// 文案里只点一份，其余只报计数 —— 否则文案会变成一份路径清单。
		if p != "" && !seen[p] {
			seen[p] = true
			if len(sample) < foreignPathSampleMax {
				sample = append(sample, p)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("读外来账本的回撤落点失败（本地记录未改动）: %w", err)
	}
	if len(badOps) == 0 {
		return nil, "", nil
	}
	return badOps, foreignPathDowngradeNote(len(badOps), sample, len(roots)), nil
}

// foreignPathSampleMax 是降级说明里最多列几个示例路径。
const foreignPathSampleMax = 3

// foreignPathDowngradeNote 组一句给界面看的降级说明。
// 两套文案（有根/无根）：无根时"请先扫一次盘"是真出路，有根时不是。
func foreignPathDowngradeNote(n int, sample []string, rootCount int) string {
	const shown = foreignPathSampleMax
	detail := strings.Join(sample[:min(len(sample), shown)], "、")
	more := ""
	if len(sample) > shown {
		more = fmt.Sprintf("（另有若干同样越界的路径未列）")
	}
	if rootCount == 0 {
		return fmt.Sprintf("%d 笔外来清理记录的落点不在本机任何扫描根之下（示例：%s%s），"+
			"已按不可回撤导入：回撤会按那些落点搬动文件并创建对应目录，而本机从未扫描过那些位置。"+
			"若要在本机回撤这些条目，请先在本机扫描一次再重新导入；"+
			"只想查看记录不受影响", n, detail, more)
	}
	return fmt.Sprintf("%d 笔外来清理记录的落点落在本机扫描根之外（示例：%s%s），"+
		"已按不可回撤导入：回撤会按那些落点搬动文件并创建对应目录，而本机从未扫描过那些位置。"+
		"若要在本机回撤这些条目，请在本机重新扫描后重新导出导入；只想查看记录不受影响",
		n, detail, more)
}

// localScanRoots 取本机全部扫描根，归一成可前缀比较的键。
// roots 存的是 JSON 数组字符串；解不开的行按"没有根"处理（跳过）——
// 那一格若发生，本机那条扫描本来也读不出根，对本闸的贡献是零。
func localScanRoots(local *sql.DB) ([]string, error) {
	rows, err := local.Query(localScanRootsSQL)
	if err != nil {
		return nil, fmt.Errorf("读本机扫描根失败（本地记录未改动）: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("读本机扫描根失败（本地记录未改动）: %w", err)
		}
		var rs []string
		if err := json.Unmarshal(raw, &rs); err != nil {
			continue // 形状不可控的一行：贡献零根，不是错误
		}
		for _, r := range rs {
			if k := pathnorm.DirKey(r, foreignPathSep); k != "" {
				out = append(out, k)
			}
		}
	}
	return out, rows.Err()
}

// localKnownPaths 取本机"见过"的精确路径键（hist_files.path + op_items.orig_path）。
// 这是 foreignPathAllowed 的条件 2/3；两处并成一张表是因为它们回答同一个问题。
func localKnownPaths(local *sql.DB) (map[string]bool, error) {
	out := make(map[string]bool)
	for _, q := range []string{`SELECT path FROM hist_files`, `SELECT orig_path FROM op_items`} {
		rows, err := local.Query(q)
		if err != nil {
			return nil, fmt.Errorf("读本机路径清单失败（本地记录未改动）: %w", err)
		}
		for rows.Next() {
			var p string
			if err := rows.Scan(&p); err != nil {
				rows.Close()
				return nil, fmt.Errorf("读本机路径清单失败（本地记录未改动）: %w", err)
			}
			if p == "" {
				continue
			}
			if k := pathnorm.DirKey(p, foreignPathSep); k != "" {
				out[k] = true
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("读本机路径清单失败（本地记录未改动）: %w", err)
		}
	}
	return out, nil
}

// foreignPathAllowed 判一条外来 orig_path 是否有本机来源。
//
// 获准的条件（三条任一）：
//  1. 解析后落在本机某条扫描根之下（正规用法：同机导出导回、或同布局的另一台机）；
//  2. 归一后与本机某条 hist_files.path 精确同值（扫描后用户搬过家目录的兜底）；
//  3. 归一后与本机某条 op_items.orig_path 精确同值（同上，另一条来源腿）。
//
// 条件 2/3 是为"本机那条扫描已被 MaxScanHistory 裁掉"留的出口——
// 裁剪删的是最旧的扫描行，而 op_items 里的回撤条目可能还在。
//
// 归一口径走 `internal/pathnorm`（Slash + DirKey），不自己写一份分隔符判据：
// M64 的教训是同一判据在本仓曾有四份实现、四份对 `\` 的取径不一致。
// 斜杠键空间里判前缀一律走 pathnorm.Under（不许自己拼 HasPrefix）。
func foreignPathAllowed(p string, roots []string, known map[string]bool) bool {
	key := pathnorm.DirKey(p, foreignPathSep)
	if key == "" {
		return false
	}
	if !absoluteKey(key) {
		return false
	}
	for _, r := range roots {
		if pathnorm.Under(key, r) {
			return true
		}
	}
	return known[key]
}

// absoluteKey 判归一后的键是不是"绝对路径"。
//
// ★ 为什么不能只认前导 `/`：账本是**跨平台**字符串，一份 Windows 账本归一后是
// `C:/Users/me/x.bin`，它不以 `/` 开头但**绝对不相对**——只认 `/` 会把
// "外来 Windows 账本 + 本机 Windows 扫描根"这一档正常用法全判成相对路径
// （本机实测踩过：P-10/P-11 两格在收进这条判据之前就是这么红的）。
//
// 三条判据任一即真：前导 `/`（POSIX 绝对，UNC `//host/share` 归一后仍以 `/`
// 开头，由这一条一并覆盖）、`X:` 盘符（Windows 绝对）。
//
// 反过来说，判否的那一档是 `relative/x.bin` 与 `C:relative`（**无斜杠**的盘符
// 形式是 Windows 的"当前盘目录"，相对性等同相对路径）——这正是要在回撤侧
// 相对当时工作目录解析、而 GUI 进程的 CWD 可能是 `/` 的那两种形状。
func absoluteKey(key string) bool {
	if strings.HasPrefix(key, "/") {
		return true // 含 UNC：//host/share 去掉尾斜杠后仍以 / 开头
	}
	if len(key) >= 2 && key[1] == ':' {
		c := key[0]
		return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	}
	return false
}

// scanKey 与 opKey 是两条自然键（裁定 R-2 的判重依据）。
//
// ★ 为什么不能只比 saved_at / created_at：同一秒内的两次扫描（自动化、重试）会撞键，
// 判成"本地已有"就静默吞掉一条真记录——那是导入功能自己能造出来的数据丢失形状。
// ★ 为什么不认 id：id 是各库自己的 AUTOINCREMENT，跨库无意义，照 id 插要么撞主键、
// 要么错关联。
// ★ 为什么 threads / paranoid / orig_files 必须在键里（M377，裁定 M375 取向①）：
// 这三列改变的是这份扫描的**语义结论**——paranoid 决定分组是否可信、orig_files 是当时
// 实际看到的读数、threads 是当时的工况。同五字段而参数不同是**两次不同的判断**，
// 判成重复不只抑制子行，还会把外来的 ops 关联到本地另一套参数跑出来的那条扫描上
// （把两份判断合并成一份）。改前的键只有五字段，真读数见 04 §6.69 的 P-39/P-40。
//
// ★ 为什么 groups_count / files_count / reclaimable **不在**键里（M383，2026-10-01 裁-3）：
// 这三列会被本仓自己的 `PruneScanFiles`（scan.go 同一条事务里重写）改掉——清理之后
// 组数、文件数、可回收量都变小，而这条扫描还是**同一条判断**。把它们放进键，等于
// "同一份判断被修剪过 ⇒ 判成新扫描"：导入侧多插一行、并把最旧那条按 M356 的裁剪淘汰掉，
// 用户看到的是一次"清理后重导，历史记录反而多了几条、旧的没了"。
// 键只装**不可派生**的量：工况（threads/paranoid/roots/filters/saved_at）与保存瞬间的
// 原始读数（orig_files）。这两半边合起来才是 M377 与本批共同的方向。
type scanKey struct {
	SavedAt   int64
	Roots     string
	Filters   string
	Threads   int64
	Paranoid  bool
	OrigFiles int64
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
// 而这个功能的全部理由就是"防止误删除"。代价如实说：导入后本地可能超过 20 条。
//
// ★★ 下一轮淘汰**吃的是谁**（M356 更正，此前此处与手册两处都写反了）：
// 外来行插入时不带 id ⇒ 一律拿最高的自增号；而 SaveScan 的裁剪按 id 保新删旧
// （`ORDER BY id DESC LIMIT -1 OFFSET 20`）⇒ **刚导进来的记录排在最新一侧，淘汰碰不到它们**，
// 被清掉的是**用户自己的本地最旧记录**。真读数由 P-10 那格钉子钉住
// （import_m355_test.go 的 TestM356TrimEvictsLowestIdLocalNotImportedRows）。
// 也就是说：导入这件事的真实代价不是"导进来的会丢"，而是"本地最旧的那条会因此提前丢"——
// 两句话在手册里必须分开写，混成一句就是另一种谎（09/10 两处已同步更正）。
// 裁剪取向本身（按 saved_at 还是按 id、外来行要不要豁免）：**2026-10-01 已裁定维持现状**
// ——按 id 保新删旧、外来行不豁免（M378，划账 04 §6.69；依据是上面那段现读 + P-10 钉子）。
// 判据、断言与手册口径都不动；要改这一格必须重新起一批并给出新证据，不许顺手改。
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

	// M355（第八轮审查 P0-1）：结构过关 ≠ 内容证据过关。外来账本里"可回撤但无从比对"
	// 的条目必须在**任何本地写入之前**拒绝，理由见 checkUndoEvidence 的注释。
	if err := checkUndoEvidence(src); err != nil {
		return sum, err
	}

	// DDP-9：内容证据那一半封住之后，**路径**那一半仍开着——ops 侧对 OrigPath 零校验，
	// 落点由外来账本说了算。这一闸同样在写之前、与上一闸同一集合（不多拦也不少拦），
	// 但**取向是降级而不是拒收**：见 markForeignUndoPaths 的注释（整单拒绝会打死
	// "把 A 机的账本导到 B 机看"这个功能的正当用途）。返回的是源侧 op_id 集合，
	// 写入时按 op.id 匹配。
	badOps, downgradeNote, err := markForeignUndoPaths(src, s.db)
	if err != nil {
		return sum, err
	}

	// M363（第八轮审查批 2）：读集合 → 判重 → 写 tx → Commit 必须在**同一个** s.mu 临界区里。
	// 本方法是 internal/history 里唯一一个不取 s.mu 的写者（非测试代码 16 处 s.mu.Lock，
	// import.go 原先一处都没有），而 Store 的注释明写"全部方法内部串行化"。
	// ⇒ 两次并发导入（或多线程 RPC）各自读到"本地没有"，同一条外来扫描被写两遍：
	//   判重依据在两次调用之间已经过期，而写它用的是**另一段**临界区都算不上的裸 tx。
	// ★ 锁加在这里而不是函数开头：前面的 openReadOnly / validateForeignLedger /
	//   checkUndoEvidence 只碰 src，握锁等于把外来文件的 IO 时间算进本地写锁；
	//   而且它们失败时直接返回，不需要串行化。区间内的 insertScanRow / mergeScanChildren /
	//   insertOpRow / mergeOpItems 全是自由函数（不取 s.mu），src 是另一条 *sql.DB
	//   ⇒ 不存在回头再拿同一把锁的路径，死锁自证见设计段 §2.2 判据 5)。
	s.mu.Lock()
	defer s.mu.Unlock()

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
			Threads: sc.threads, Paranoid: sc.paranoid, OrigFiles: sc.origFiles}
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
		// M371（§3.2e）：孤儿是一**格归属**，不是一格计数动作——先记下"指不到东西"这个事实，
		// 等这一条真的要落库时才计进回执。改前 `sum.OpsOrphaned++` 就地加，而去重跳过
		// （下面那个 `continue`）在它之后才发生 ⇒ 同一份影像导第二次时一行都没新增，
		// 回执却写着"发现 N 条孤儿"（一份自相矛盾的收据，前端原样展示）。
		orphan := false
		if op.histID != 0 {
			local, ok := scanMap[op.histID]
			if !ok {
				// 源里这条 op 指向一个源库自己没有的扫描 ⇒ 无从映射，置 0 并如实记账。
				orphan = true
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
		if orphan {
			sum.OpsOrphaned++
		}
		// DDP-9：落点越界的外来可回撤记录按**不可撤**落库。降级写在同一事务里——
		// 与 insertOpRow 分两次提交的话，中途失败会留下"op 行已落、undoable 还没改"的
		// 窗口，而那个窗口里它是可撤的（威胁正是"可撤"）。回撤腿一行代码都没动。
		if badOps[op.id] {
			if _, err := tx.Exec(`UPDATE op_records SET undoable = 0 WHERE id = ?`, opID); err != nil {
				return sum, fmt.Errorf("把越界落点的外来记录标成不可撤失败（本地记录未改动）: %w", err)
			}
			sum.OpsUndoDowngraded++
		}
		localOps[k] = opID
		sum.OpsAdded++
		if err := mergeOpItems(tx, src, op.id, opID); err != nil {
			return sum, err
		}
	}
	if err := tx.Commit(); err != nil {
		return sum, err
	}
	// 降级说明只在真有降级时非空；贴在回执上让界面能显示"导入了、有 N 笔不能回撤"。
	sum.OpsUndoDowngradeNote = downgradeNote
	hardenSidecars(s.path) // M379：写腿收口处补档（裁-1「写入腿各补收紧」)
	return sum, nil
}

// localScanKeys 一条集合式语句读回本地全部扫描自然键 → 行 id。
// 重复键（历史遗留或并发写造成）保留 id 最小的那条：判重只看存在性，
// 而"指到哪一条"要有一个不随查询计划变化的确定答案。
//
// ★ 这里 SELECT 的列**就是键的列**（M383）：groups_count / files_count / reclaimable
//
//	已随键一起出局，不要"顺手补回来"——它们是 PruneScanFiles 会重写的派生值，
//	进键即"修剪过的同一条判断算成新扫描"。
func (s *Store) localScanKeys() (map[scanKey]int64, error) {
	rows, err := s.db.Query(`SELECT id, saved_at, roots, filters, threads, paranoid, orig_files
		FROM scan_history ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[scanKey]int64)
	for rows.Next() {
		var (
			id             int64
			k              scanKey
			roots, filters []byte
		)
		if err := rows.Scan(&id, &k.SavedAt, &roots, &filters,
			&k.Threads, &k.Paranoid, &k.OrigFiles); err != nil {
			return nil, err
		}
		k.Roots, k.Filters = string(roots), string(filters)
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

// blobArg 把驱动读回的 nil 还原成"零长 BLOB"再交给 INSERT。
//
// ★ 这是一条实测的**往返不对称**，不是顺手兜一下（探针 P-4/P-5 的红因，读数见 §6.67）：
// modernc 驱动把参数里的 []byte{} 编成 X”（typeof=blob），却把 X” **读回**成 nil；
// nil 再写出去就是 NULL ⇒ 而本地 op_items.hash 是 NOT NULL
// ⇒ 一条本来合法的外来记录会撞在英文驱动串上、整单回滚。
// 搬运的口径是"照原样"：源里是零长 BLOB，本地就该是零长 BLOB。
//
// 读侧无法区分 NULL 与 X”（两者都折成 nil），因此外来文件给 NULL 时也按零长 BLOB 落地。
// 这不放松安全性：闸（checkUndoEvidence）问的是**回撤会吃到的那一集合**，
// 而 NULL 与 X” 在回撤判据上是同一档（都经 copy 折成零值数组）——那一集合里的两者
// 已经整单拒收，落到这里的只剩回撤永远选不中的行。
//
// ★ 扫描侧的 hist_groups.hash 有**同一个**缺陷，本批刻意不修（拟登记）：那条腿现在是
// import_m352_test.go 里 TestImportMidChildFailureRollsBack 的**夹具支点**（它靠
// "外来 hist_groups.hash 给 NULL ⇒ 撞本地 NOT NULL"来构造"子行插到一半失败"）。
// 一起归一化会把那条回滚用例的前提抽掉，而重造它的失败形状属于改既有测试的夹具——
// 与"本批只加判据"的动面不是一回事，留作单独一批处理。
func blobArg(b []byte) []byte {
	if b == nil {
		return []byte{}
	}
	return b
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
			newOpID, it.origPath, it.destPath, it.linkSrc, blobArg(it.hash), it.size, it.mtimeNs,
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
