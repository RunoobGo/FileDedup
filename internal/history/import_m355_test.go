package history

// M355（第八轮全面审查 P0-1，设计段 §1）：外来账本**不得**让导入行拿到
// "旧记录无从比对"的回撤豁免。
//
// 形状：导入侧只问列名不问值，op_items.hash 给 X'' 也照样入库；而回撤腿的
// undoSourceCheck 恰好在 hash 为零值时**同时**跳过全量 BLAKE3 与身份复核，
// 只剩一条 size 比对——size 是外来账本自己填的。于是"来历不明的 .db + 点一次
// 全部回撤"= 按外来路径搬走本机真实文件。
//
// 判据只收在"回撤真会吃到的那一集合"（undoable=1 且 state ∈ {done, undo_failed}），
// 所以 P-4/P-5 两条负控制与 P-1/P-2 同批存在：多拦一分就是过度拒绝，
// 而"用全表拦截换一条绿"正是本仓反复点名的假修法。
//
// 全部走**真实导出件**（src.ExportTo 的 .db 影像）作源，沿用 import_m352_test.go
// 那一族的理由：手搓源库会让"导出的东西导不回来"这一格永远绿着。

import (
	"bytes"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/model"
)

// mkOpRawEvid 落一笔清理记录，逐字段可控（undoable / state / 内容证据长度都可调），
// 用于造"升级前的老账本"那一档：它的条目没有内容证据。
//
// ★ 空证据必须写成 SQL 字面量 X”，不能传 Go 的 []byte{}：驱动把零长切片编码成
// **NULL**，而本地建表里 op_items.hash 是 NOT NULL ⇒ 夹具会在自己这一步就撞约束，
// 于是"拒收"这一格红在夹具、"放行"那一格红在约束错误（本轮实测踩过，读数见 §6.67）。
// X”（零长 BLOB）才是老账本的真实形状：它在 NOT NULL 下合法，读回来经
// oplog.go 的 copy(it.Hash[:], hb) 折成零值数组 ⇒ 正是要拦的那一条豁免。
//
// ★ 每条路径开的句柄一律 Close（M335 的 TempDir 占用教训，本轮普查又抓到两处同类）。
func mkOpRawEvid(t *testing.T, s *Store, createdAt int64, kind, targetDir string,
	histID int64, undoable bool, state string, hash []byte) int64 {
	t.Helper()
	res, err := s.db.Exec(`INSERT INTO op_records
		(op_kind, created_at, target_dir, hist_id, undoable, done_count, reclaimed)
		VALUES (?, ?, ?, ?, ?, 1, 1024)`, kind, createdAt, targetDir, histID, undoable)
	if err != nil {
		t.Fatalf("夹具落 op_records 失败: %v", err)
	}
	opID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("夹具取 op id 失败: %v", err)
	}
	// 占位符个数随分支变 ⇒ 实参必须同步条件构造：无参分支多传一个 nil 会把 state 顶成
	// NULL，夹具先撞 op_items.state 约束（本轮实测红在夹具而非判据）。
	hashSQL := "X''"
	args := []any{opID, filepath.Join(targetDir, "victim.bin")}
	if len(hash) > 0 {
		hashSQL = "?"
		args = append(args, hash)
	}
	args = append(args, state)
	if _, err := s.db.Exec(`INSERT INTO op_items
		(op_id, orig_path, dest_path, link_src, hash, size, mtime_ns, state, err)
		VALUES (?, ?, '', '', `+hashSQL+`, 1024, 7, ?, '')`, args...); err != nil {
		t.Fatalf("夹具落 op_items 失败: %v", err)
	}
	return opID
}

// countRows 只用于"账本没增长"这一格：拒绝导入后本地五表必须连条数都没变。
func countRows(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("数 %s 失败: %v", table, err)
	}
	return n
}

// srcWithOp 造一份"含一笔指定证据形状的清理记录"的真实导出影像。
func srcWithOp(t *testing.T, name string, undoable bool, state string, hash []byte) string {
	t.Helper()
	src := newStoreAt(t, t.TempDir(), "src.db")
	histID := mkScanAt(t, src, 1700000200, "/theirs/c", 1)
	mkOpRawEvid(t, src, 1700000900, "trash", "/theirs/trash", histID, undoable, state, hash)
	return exportImage(t, src, name)
}

// foreignDBFixture 手搓一份外来账本文件：五张表的**列名**逐张对齐 importRequired，
// 但不带 NOT NULL 约束（外来文件的形状本来就不可控），再按 data 逐条灌数据。
//
// ★ 为什么这一族不能只走 exportImage：产品自己写不出 NULL / 零长 BLOB 这些形状
//
//	（本地建表两套都是 NOT NULL），而"外来文件"能——前置校验只看列名，所以这些形状
//	正是闸要面对的输入。沿用 import_m352_test.go 里
//	TestImportMidChildFailureRollsBack 的写法；★ 句柄每条路径都 Close（M335 教训）。
func foreignDBFixture(t *testing.T, name string, data []string) string {
	t.Helper()
	srcPath := filepath.Join(t.TempDir(), name)
	db, err := sql.Open("sqlite", srcPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range append([]string{
		`CREATE TABLE scan_history (id INTEGER PRIMARY KEY AUTOINCREMENT, saved_at INTEGER,
			roots TEXT, filters TEXT, threads INTEGER, paranoid INTEGER, groups_count INTEGER,
			files_count INTEGER, orig_files INTEGER, reclaimable INTEGER, failed_json TEXT, keep_paths TEXT)`,
		`CREATE TABLE hist_groups (id INTEGER PRIMARY KEY AUTOINCREMENT, hist_id INTEGER,
			hash BLOB, size INTEGER, reclaimable INTEGER, ord INTEGER)`,
		`CREATE TABLE hist_files (id INTEGER PRIMARY KEY AUTOINCREMENT, hist_id INTEGER,
			group_id INTEGER, path TEXT, size INTEGER, mtime_ns INTEGER)`,
		`CREATE TABLE op_records (id INTEGER PRIMARY KEY AUTOINCREMENT, op_kind TEXT,
			created_at INTEGER, target_dir TEXT, hist_id INTEGER, undoable INTEGER,
			done_count INTEGER, reclaimed INTEGER)`,
		`CREATE TABLE op_items (id INTEGER PRIMARY KEY AUTOINCREMENT, op_id INTEGER, orig_path TEXT,
			dest_path TEXT, link_src TEXT, hash BLOB, size INTEGER, mtime_ns INTEGER, state TEXT, err TEXT)`,
	}, data...) {
		if _, err := db.Exec(q); err != nil {
			db.Close()
			t.Fatalf("夹具造失败: %v", err)
		}
	}
	db.Close()
	return srcPath
}

// nullHashForeignDB 造一份 hash 给了 **NULL** 的外来账本。
//
// NULL 是这条闸最容漏的一档——SQLite 里 length(NULL) 返回 NULL，
// `NULL <> 32` 的判定结果也是 NULL 而非真 ⇒ **只写长度比较的 SQL 会把这一行放行**。
func nullHashForeignDB(t *testing.T, name string) string {
	t.Helper()
	return foreignDBFixture(t, name, []string{
		`INSERT INTO scan_history (saved_at, roots, filters, threads, paranoid, groups_count,
			files_count, orig_files, reclaimable, failed_json, keep_paths)
			VALUES (1700000200, '["/nullhash/c"]', '{}', 4, 0, 0, 0, 0, 0, '[]', '[]')`,
		`INSERT INTO op_records (op_kind, created_at, target_dir, hist_id, undoable, done_count, reclaimed)
			VALUES ('trash', 1700000900, '/nullhash/trash', 1, 1, 1, 1024)`,
		`INSERT INTO op_items (op_id, orig_path, dest_path, link_src, hash, size, mtime_ns, state, err)
			VALUES (1, '/nullhash/trash/victim.bin', '', '', NULL, 1024, 7, 'done', '')`,
	})
}

// emptyGroupHashForeignDB 造一份"重复组的 hash 是零长 BLOB"的外来账本（扫描侧，不进回撤选择集）。
// 供 P-10 那条**现读钉子**使用。
func emptyGroupHashForeignDB(t *testing.T, name string) string {
	t.Helper()
	return foreignDBFixture(t, name, []string{
		`INSERT INTO scan_history (saved_at, roots, filters, threads, paranoid, groups_count,
			files_count, orig_files, reclaimable, failed_json, keep_paths)
			VALUES (1700000300, '["/emptygroup/c"]', '{}', 4, 0, 1, 1, 1, 10, '[]', '[]')`,
		`INSERT INTO hist_groups (hist_id, hash, size, reclaimable, ord) VALUES (1, X'', 10, 10, 0)`,
		`INSERT INTO hist_files (hist_id, group_id, path, size, mtime_ns)
			VALUES (1, 1, '/emptygroup/c/a.bin', 10, 1)`,
	})
}

// P-1 空内容证据 + undoable=1 + state=done ⇒ 整单拒绝，本地一字未动。
// 修前必红的一格：改前 ImportFrom 返回 err=nil、OpsAdded=1。
func TestM355ImportRefusesUndoableItemWithoutContentEvidence(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	mkScanAt(t, local, 1700000000, "/mine/a", 1)
	localBefore := ledgerDump(t, local)

	image := srcWithOp(t, "ledger-empty.db", true, StateDone, []byte{})

	sum, err := local.ImportFrom(image)
	if err == nil {
		t.Fatalf("M355：外来条目没有内容证据却被接受了（回执 %+v）⇒ 导入行拿到了回撤豁免", sum)
	}
	if !containsCJK(err.Error()) {
		t.Errorf("M355：拒绝文案必须是中文（全仓口径），实得 %v", err)
	}
	if !strings.Contains(err.Error(), "内容证据") {
		t.Errorf("M355：拒绝文案要点名「缺内容证据」这件事，实得 %v", err)
	}
	if !strings.Contains(err.Error(), "本地记录未改动") {
		t.Errorf("M355：拒绝文案必须交代本地没动，实得 %v", err)
	}
	if sum != (ImportSummary{}) {
		t.Errorf("M355：拒绝时回执必须全零（半本账不算导成），实得 %+v", sum)
	}
	after := ledgerDump(t, local)
	ledgerUnchanged(t, localBefore, after)
	if len(after) != len(localBefore) {
		t.Errorf("M355：拒绝导入后本地行数仍变了（before=%d after=%d）", len(localBefore), len(after))
	}
	if n := countRows(t, local, "op_records"); n != 0 {
		t.Errorf("M355：被拒绝的清理记录却有 %d 行落进本地", n)
	}
}

// P-2 长度不足（31 字节）同样拒：判据是"有没有满长证据"，不是"是不是全零"。
// 改前 GetOp 的 copy 会把短切片**静默零填充**成零值数组，这一格正是防它过关。
func TestM355ImportRefusesShortHashItem(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	before := ledgerDump(t, local)
	image := srcWithOp(t, "ledger-short.db", true, StateDone, bytes.Repeat([]byte{0xAB}, 31))

	if _, err := local.ImportFrom(image); err == nil {
		t.Fatal("M355：31 字节的短哈希被当成正经内容证据收了进来")
	} else if !strings.Contains(err.Error(), "内容证据") {
		t.Errorf("M355：短哈希的拒绝理由走偏，实得 %v", err)
	}
	ledgerUnchanged(t, before, ledgerDump(t, local))
}

// P-3 正控制：满 32 字节照收（闸不许把正常导出件也挡掉）。
func TestM355ImportAcceptsItemWithFullContentEvidence(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	mkScanAt(t, local, 1700000000, "/mine/a", 1)
	image := srcWithOp(t, "ledger-good.db", true, StateDone,
		bytes.Repeat([]byte{0x7F}, 32))

	sum, err := local.ImportFrom(image)
	if err != nil {
		t.Fatalf("满长证据的正常记录被拒了: %v", err)
	}
	if sum.OpsAdded != 1 {
		t.Errorf("M355：正控制回执失真 %+v，want OpsAdded=1", sum)
	}
	var got []byte
	if err := local.db.QueryRow(`SELECT hash FROM op_items ORDER BY id DESC LIMIT 1`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 32 {
		t.Errorf("M355：搬进本地的哈希长度 %d ≠ 32，搬运途中被改", len(got))
	}
}

// P-4 负控制：不可撤的记录没有内容证据 ⇒ 照收。拦它是过度拒绝。
func TestM355ImportAllowsNonUndoableItemWithoutEvidence(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	image := srcWithOp(t, "ledger-nonundo.db", false, StateDone, []byte{})

	sum, err := local.ImportFrom(image)
	if err != nil {
		t.Fatalf("M355：undoable=0 的记录没能导入（%v）⇒ 要么闸超出了回撤选择集，要么搬运把零长 BLOB 变成了 NULL", err)
	}
	if sum.OpsAdded != 1 {
		t.Errorf("M355：负控制回执失真 %+v", sum)
	}
}

// P-5 负控制：可撤记录里"已经撤成"的条目（state=undone）没有证据 ⇒ 照收。
// ★ 这一格钉的是判据与 app_ops.go 回撤选择集**逐字同一集合**：
//
//	if it.State == history.StateDone || it.State == history.StateUndoFailed
//
// 变异用：把 state 放宽成"全表都要证据"，P-4/P-5 必红（设计段 §1.4 的 MU-2）。
func TestM355ImportAllowsAlreadyUndoneItemWithoutEvidence(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	image := srcWithOp(t, "ledger-undone.db", true, StateUndone, []byte{})

	if _, err := local.ImportFrom(image); err != nil {
		t.Fatalf("M355：已撤成条目没能导入（%v）⇒ 要么闸管到了回撤吃不到的行，要么搬运把零长 BLOB 变成了 NULL", err)
	}
}

// P-6 undo_failed 也在回撤选择集内（APP-4 明确放行重试），所以它同样要证据。
func TestM355ImportRefusesUndoFailedItemWithoutContentEvidence(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	image := srcWithOp(t, "ledger-undofailed.db", true, StateUndoFailed, []byte{})

	if _, err := local.ImportFrom(image); err == nil {
		t.Fatal("M355：undo_failed 条目没有内容证据却被收下——这一档「修正后重试」会直接搬文件")
	}
}

// P-7 满 32 字节但**整串全零**也拒。
// ★ 这一格是实施时给设计判据补的一支（原判据只写 `length(hash) <> 32`，见 §1.6 追记）：
// 读腿 oplog.go 的 copy(it.Hash[:], hb) 把这行折成 [32]byte{} 零值 ⇒ 走的正是 undo.go
// "零值哈希跳过比对与身份复核"那条豁免。只量长度的闸在这里会**绿着放过同一个洞**。
func TestM355ImportRefusesAllZeroHashItem(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	before := ledgerDump(t, local)
	image := srcWithOp(t, "ledger-allzero.db", true, StateDone, make([]byte, 32))

	if _, err := local.ImportFrom(image); err == nil {
		t.Fatal("M355：全零的 32 字节哈希被当成正经内容证据收了进来 ⇒ 导入行拿到回撤豁免")
	} else if !strings.Contains(err.Error(), "内容证据") {
		t.Errorf("M355：全零哈希的拒绝理由走偏，实得 %v", err)
	}
	ledgerUnchanged(t, before, ledgerDump(t, local))
	if n := countRows(t, local, "op_records"); n != 0 {
		t.Errorf("M355：被拒绝的清理记录却有 %d 行落进本地", n)
	}
}

// P-8 外来文件的 hash 给 NULL ⇒ 同样拒。
// 钉的是 SQL 三值逻辑那一格：`length(NULL) <> 32` 判定成 NULL（不是真），
// 只写长度比较的语句会把这行当"有证据"放行。
func TestM355ImportRefusesNullHashItem(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	mkScanAt(t, local, 1700000000, "/mine/a", 1)
	before := ledgerDump(t, local)

	if _, err := local.ImportFrom(nullHashForeignDB(t, "nullhash.db")); err == nil {
		t.Fatal("M355：hash 为 NULL 的外来条目被放行了 ⇒ 判据被 SQL 三值逻辑绕过")
	} else if !strings.Contains(err.Error(), "内容证据") {
		t.Errorf("M355：NULL 的拒绝理由走偏，实得 %v", err)
	}
	ledgerUnchanged(t, before, ledgerDump(t, local))
	if n := countRows(t, local, "op_records"); n != 0 {
		t.Errorf("M355：被拒绝的清理记录却有 %d 行落进本地", n)
	}
}

// P-9 **现读钉子**（扫描侧那条腿的同一缺陷，本批登记不修）：外来重复组的零长 BLOB
// 今天**导不进来**——整单撞在 hist_groups.hash 的 NOT NULL 约束上。
//
// 为什么不顺手修掉：mergeScanChildren 这条腿是 import_m352_test.go 里
// TestImportMidChildFailureRollsBack 的**夹具支点**（那条用例靠"外来 hist_groups.hash 给
// NULL ⇒ 撞本地 NOT NULL"构造"子行插到一半失败"）。一起归一化就把那条回滚用例的前提抽掉了，
// 而重造它的失败形状 = 改既有测试的夹具，与本批"只加判据"的动面不是一回事 ⇒ 单独一批。
//
// ★ 这条**改前就绿、改后仍绿**（本批没动扫描侧），它是钉住现读行为的钉子：
// 将来修那一腿时必须把这条断言**反过来**，届时这一格就是变异自检。
func TestM355ImportScanSideEmptyHashStillDiesOnConstraint(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	before := ledgerDump(t, local)
	image := emptyGroupHashForeignDB(t, "emptygroup.db")

	if _, err := local.ImportFrom(image); err == nil {
		t.Fatal("M355：扫描侧的零长 hash 现在能导进来了 ⇒ 那一腿的归一化已经做掉，" +
			"本钉子要反写（断「照原样搬成 blob」），且 m352 的回滚夹具要重造")
	}
	// 红也要红得干净：整笔回滚，本地一字不动（这条同时是本钉子的前提自检）
	ledgerUnchanged(t, before, ledgerDump(t, local))
	if n := countRows(t, local, "hist_groups"); n != 0 {
		t.Errorf("M355：约束失败却留下了半截重复组（%d 行）", n)
	}
}

// P-10 **真值钉子**（拟 M356，设计段 §1.2）：钉住"下一次扫描收尾淘汰谁"。
//
// ★ 这条**改前就绿**，它是防漂的钉子而不是修前红探针——按 §6.11 那批的口径如实标注。
// 它存在的全部理由：手册与代码注释三处写成"刚导进来的记录若排在最旧一侧会被淘汰"，
// 而外来行不带 id 插入 ⇒ 必然拿最高自增号 ⇒ 按 id 保新删旧的淘汰**碰不到它们**，
// 被清掉的是用户自己的本地最旧记录。取向本身待裁（拟 M357），本条只钉现读行为。
func TestM356TrimEvictsLowestIdLocalNotImportedRows(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	// 本地灌满 20 条（id 1..20），时间戳各不相同、roots 各异 ⇒ 自然键互不相同
	for i := 0; i < MaxScanHistory; i++ {
		mkScanAt(t, local, int64(1700000000+i*10), fmt.Sprintf("/mine/r%02d", i), 1)
	}
	if n := countRows(t, local, "scan_history"); n != MaxScanHistory {
		t.Fatalf("夹具没铺满：本地 %d 条，want %d", n, MaxScanHistory)
	}
	var localOldestID, localNewestID int64
	if err := local.db.QueryRow(`SELECT MIN(id), MAX(id) FROM scan_history`).Scan(&localOldestID, &localNewestID); err != nil {
		t.Fatal(err)
	}

	src := newStoreAt(t, t.TempDir(), "src.db")
	mkScanAt(t, src, 1700090000, "/theirs/x", 1)
	mkScanAt(t, src, 1700090010, "/theirs/y", 1)
	if _, err := local.ImportFrom(exportImage(t, src, "trim.db")); err != nil {
		t.Fatalf("导入夹具失败: %v", err)
	}
	var importedMin int64
	if err := local.db.QueryRow(`SELECT MIN(id) FROM scan_history WHERE roots LIKE '%theirs%'`).Scan(&importedMin); err != nil {
		t.Fatal(err)
	}
	// ★ 前提问的是"外来行拿到了比**所有本地行**都高的 id"（不带 id 插入 ⇒ 走自增）。
	// 首跑把这条写成了 importedMin > localOldest+20，off-by-one 判在 21 上 ⇒ 钉子红在夹具；
	// 前提自检红和判据红必须能分开（§6.14 那条"取红必须验红的是不是那一格"）。
	if importedMin <= localNewestID {
		t.Fatalf("夹具前提不成立：外来行没拿到比本地更大的 id（importedMin=%d localNewest=%d）⇒ 本钉子的判据走空", importedMin, localNewestID)
	}

	// 再走一次**真实** SaveScan（裁剪只在这条路上发生）
	if _, err := local.SaveScan(model.ScanConfig{
		Roots: []string{"/mine/next"},
	}, nil, nil); err != nil {
		t.Fatalf("SaveScan 失败: %v", err)
	}

	var stillThere int
	if err := local.db.QueryRow(`SELECT COUNT(*) FROM scan_history WHERE roots LIKE '%theirs%'`).Scan(&stillThere); err != nil {
		t.Fatal(err)
	}
	if stillThere != 2 {
		t.Errorf("M356：导入的两条在外来行侧被淘汰了（实得 %d 条）⇒ 淘汰键不再是 id", stillThere)
	}
	if err := local.db.QueryRow(`SELECT COUNT(*) FROM scan_history WHERE id = ?`, localOldestID).Scan(&stillThere); err != nil {
		t.Fatal(err)
	}
	if stillThere != 0 {
		t.Errorf("M356：本地最小 id 那条没被淘汰 ⇒ 现读行为变了，三处话术要重取（id=%d）", localOldestID)
	}
	var total int
	if err := local.db.QueryRow(`SELECT COUNT(*) FROM scan_history`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != MaxScanHistory {
		t.Errorf("M356：收尾后应恰好留 %d 条，实得 %d", MaxScanHistory, total)
	}
}
