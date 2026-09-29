package history

// M352 导入记录的判据（2026-09-29 设计段 §6.3）。
//
// 全部走**真实导出件**（src.ExportTo 的 .db 影像）作为导入源，不用手搓 SQL 造源库：
// 「导出→导入」是这个功能唯一存在的理由，判据必须打在两件的真接缝上；手搓源库会让
// "导出的东西导不回来"这一格永远绿着。
//
// 夹具里对 `s.db` 直接写是为了**钉死时间戳**（SaveScan 用 time.Now）：
// "同一秒两条扫描"这一格只能靠造数据复现，而它正是自然键少一列时会静默吞记录的那一档。

import (
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// mkScanAt 用指定 saved_at 落一条扫描历史（roots/filters/组数都从 seed 派生，可控可比）。
func mkScanAt(t *testing.T, s *Store, savedAt int64, root string, groups int) int64 {
	t.Helper()
	var files, reclaim uint64
	rootsJS := `["` + root + `"]`
	filtersJS := `{"minSize":1024,"includeExts":[]}`
	for i := 0; i < groups; i++ {
		files += 3
		reclaim += uint64(2000 + i)
	}
	res, err := s.db.Exec(`INSERT INTO scan_history
		(saved_at, roots, filters, threads, paranoid, groups_count, files_count, orig_files, reclaimable, failed_json, keep_paths)
		VALUES (?, ?, ?, 4, 0, ?, ?, ?, ?, '[]', '[]')`,
		savedAt, rootsJS, filtersJS, groups, files, files, reclaim)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	for ord := 0; ord < groups; ord++ {
		var h [32]byte
		h[0] = byte(ord + 1)
		gres, err := s.db.Exec(`INSERT INTO hist_groups (hist_id, hash, size, reclaimable, ord)
			VALUES (?, ?, ?, ?, ?)`, id, h[:], 1000+ord, 2000+ord, ord)
		if err != nil {
			t.Fatal(err)
		}
		gid, err := gres.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		for j := 0; j < 3; j++ {
			if _, err := s.db.Exec(`INSERT INTO hist_files (hist_id, group_id, path, size, mtime_ns)
				VALUES (?, ?, ?, ?, ?)`,
				id, gid, filepath.Join(root, string(rune('a'+ord)), string(rune('0'+j))+".bin"),
				uint64(1000+ord), int64(1234567+j)); err != nil {
				t.Fatal(err)
			}
		}
	}
	return id
}

// mkOpAt 用指定 created_at 落一笔清理记录（含 1 条 done 明细）。
func mkOpAt(t *testing.T, s *Store, createdAt int64, kind, targetDir string, histID int64, done, reclaimed int64) int64 {
	t.Helper()
	res, err := s.db.Exec(`INSERT INTO op_records
		(op_kind, created_at, target_dir, hist_id, undoable, done_count, reclaimed)
		VALUES (?, ?, ?, ?, 1, ?, ?)`, kind, createdAt, targetDir, histID, done, reclaimed)
	if err != nil {
		t.Fatal(err)
	}
	opID, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	var h [32]byte
	h[1] = byte(done)
	if _, err := s.db.Exec(`INSERT INTO op_items
		(op_id, orig_path, dest_path, link_src, hash, size, mtime_ns, state, err)
		VALUES (?, ?, '', '', ?, 1024, 7, 'done', '')`,
		opID, filepath.Join(targetDir, "victim.bin"), h[:]); err != nil {
		t.Fatal(err)
	}
	return opID
}

// ledgerDump 是"本地一行没动"的对照物：五张表每行按 `表名:id` 折成一行文本。
//
// 为什么按行 keyed 而不是整本文本比对：导入本身会**新增**行，整本比对要么假红
// （新增行让全文不等），要么只能退化成"条数相等"——而条数挡不住 UPDATE 改写内容。
// keyed 之后判据是精确的两件事：before 的每个键 after 仍在（没删）、值一字不差（没改）。
func ledgerDump(t *testing.T, s *Store) map[string]string {
	t.Helper()
	out := make(map[string]string)
	tables := []struct {
		name  string
		query string
	}{
		{"scan_history", `SELECT id, saved_at, roots, filters, threads, paranoid, groups_count,
			files_count, orig_files, reclaimable, failed_json, keep_paths FROM scan_history`},
		{"hist_groups", `SELECT id, hist_id, hash, size, reclaimable, ord FROM hist_groups`},
		{"hist_files", `SELECT id, hist_id, group_id, path, size, mtime_ns FROM hist_files`},
		{"op_records", `SELECT id, op_kind, created_at, target_dir, hist_id, undoable,
			done_count, reclaimed FROM op_records`},
		{"op_items", `SELECT id, op_id, orig_path, dest_path, link_src, hash, size, mtime_ns,
			state, err FROM op_items`},
	}
	for _, tb := range tables {
		rows, err := s.db.Query(tb.query + " ORDER BY id ASC")
		if err != nil {
			t.Fatal(err)
		}
		cols, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			var b strings.Builder
			for i, v := range vals {
				if i > 0 {
					b.WriteString(" ")
				}
				switch x := v.(type) {
				case []byte:
					b.WriteString(hex.EncodeToString(x))
				case string:
					b.WriteString(x)
				case int64:
					fmt.Fprintf(&b, "%d", x)
				case nil:
					b.WriteString("<NULL>")
				default:
					t.Fatalf("夹具遇到未预期的列类型 %T（补进这里的分支，别放过）", v)
				}
			}
			key := fmt.Sprintf("%s:%d", tb.name, vals[0].(int64))
			if _, dup := out[key]; dup {
				t.Fatalf("夹具前提走样：键重复 %s", key)
			}
			out[key] = b.String()
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
	}
	return out
}

// ledgerUnchanged 断言 after 完整保留 before 的每一行、且值一字不差（不禁止新增行）。
// ★ 这是裁定 R-1「导入不是恢复，是合并」里"不删不改"那一半的唯一可执行形状：
//
//	条数断言（after >= before）在"删一条 + 加一条"时照样绿，而那正是最坏的失败。
func ledgerUnchanged(t *testing.T, before, after map[string]string) {
	t.Helper()
	var bad []string
	for k, v := range before {
		got, ok := after[k]
		if !ok {
			bad = append(bad, k+" 整行不见了（导入删了本地行）")
			continue
		}
		if got != v {
			bad = append(bad, k+" 内容变了 before=["+v+"] after=["+got+"]（导入改了本地行）")
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Errorf("M352：本地已有记录被改动\n%s", strings.Join(bad, "\n"))
	}
}

// exportImage 把 src 导成一份真实影像文件，返回路径。
func exportImage(t *testing.T, src *Store, name string) string {
	t.Helper()
	dest := filepath.Join(t.TempDir(), name)
	if _, err := src.ExportTo(dest); err != nil {
		t.Fatalf("夹具的导出失败: %v", err)
	}
	return dest
}

// ---- 判据 ----

// §6.3-1 增量语义：本地 2 条 + 外来 3 条（其中 1 条自然键与本地相同）⇒ 本地变 4 条，
// 且**本地原有那 2 条一行没动**。
func TestImportMergesIncrementally(t *testing.T) {
	dir := t.TempDir()
	local := newStoreAt(t, dir, "local.db")
	src := newStoreAt(t, t.TempDir(), "src.db")

	// 本地：两条固定时间戳的扫描
	mkScanAt(t, local, 1700000000, "/mine/a", 2)
	mkScanAt(t, local, 1700000100, "/mine/b", 1)
	localBefore := ledgerDump(t, local)

	// 外来：一条与本地 1700000000 那条自然键相同 + 两条新的
	mkScanAt(t, src, 1700000000, "/mine/a", 2)
	mkScanAt(t, src, 1700000200, "/theirs/c", 3)
	mkScanAt(t, src, 1700000300, "/theirs/d", 1)
	image := exportImage(t, src, "ledger.db")

	sum, err := local.ImportFrom(image)
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if sum.ScansAdded != 2 || sum.ScansSkipped != 1 {
		t.Errorf("M352：回执失真 %+v，want added=2 skipped=1", sum)
	}
	scans, err := local.ListScans()
	if err != nil {
		t.Fatal(err)
	}
	if len(scans) != 4 {
		t.Errorf("M352：本地应有 4 条（2 原有 + 2 新增），实际 %d", len(scans))
	}
	// ★ 本地原有行逐字段不变（约束 1），且确实只多出新增的那几条
	after := ledgerDump(t, local)
	ledgerUnchanged(t, localBefore, after)
	if len(after) <= len(localBefore) {
		t.Errorf("M352：账本没有增长（before=%d after=%d），新增的行不知去了哪", len(localBefore), len(after))
	}
}

// ★ 自然键少一列就会静默吞记录的那一档：同一秒、不同 roots 的两条必须都进来。
// 变异用：把 scanKey 只留 saved_at ⇒ 这一格红。
func TestImportSameSecondDifferentRoots(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	src := newStoreAt(t, t.TempDir(), "src.db")
	const sameSecond = 1700000500
	mkScanAt(t, src, sameSecond, "/theirs/x", 1)
	mkScanAt(t, src, sameSecond, "/theirs/y", 1)
	mkScanAt(t, src, sameSecond, "/theirs/z", 2)
	image := exportImage(t, src, "ledger.db")

	sum, err := local.ImportFrom(image)
	if err != nil {
		t.Fatal(err)
	}
	if sum.ScansAdded != 3 || sum.ScansSkipped != 0 {
		t.Errorf("M352：同秒三条被当成重复吞了 %+v（自然键必须含 roots/filters/计数）", sum)
	}
	// 同一份文件里再导一次：三条都应判成重复（同文件内部的重复也要认）
	if _, err := local.ImportFrom(image); err != nil {
		t.Fatal(err)
	}
	scans, _ := local.ListScans()
	if len(scans) != 3 {
		t.Errorf("M352：二次导入翻倍了，剩 %d 条 want 3", len(scans))
	}
}

// §6.3-2 幂等：同一份文件连导两次 ⇒ 第二次全零增长。
func TestImportIsIdempotent(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	src := newStoreAt(t, t.TempDir(), "src.db")
	mkScanAt(t, local, 1700000000, "/mine/a", 1)
	mkScanAt(t, src, 1700000100, "/theirs/c", 2)
	opID := mkOpAt(t, src, 1700000150, "trash", "/theirs/c", 1, 1, 1024)
	_ = opID
	image := exportImage(t, src, "ledger.db")

	first, err := local.ImportFrom(image)
	if err != nil {
		t.Fatal(err)
	}
	if first.ScansAdded != 1 || first.OpsAdded != 1 {
		t.Errorf("M352：首次导入没长 %+v", first)
	}
	before := ledgerDump(t, local)
	second, err := local.ImportFrom(image)
	if err != nil {
		t.Fatalf("第二次导入报错（幂等腿断了）: %v", err)
	}
	if second.ScansAdded != 0 || second.OpsAdded != 0 ||
		second.ScansSkipped != 1 || second.OpsSkipped != 1 {
		t.Errorf("M352：第二次导入仍 increased %+v，want 全 0 增 / 各 1 跳", second)
	}
	after := ledgerDump(t, local)
	ledgerUnchanged(t, before, after)
	if len(after) != len(before) {
		t.Errorf("M352：二次导入多出了行（before=%d after=%d）——子行没跟着父行一起判重 ⇒ 会翻倍",
			len(before), len(after))
	}
}

// §6.3-3 子行重映射：导入后 hist_files.group_id 必须指向**本次新建**的 hist_groups.id。
// 做错的样子是"文件挂到本地别人的组上"——列表一切正常，展开内容才露馅。
func TestImportRemapsChildIds(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	src := newStoreAt(t, t.TempDir(), "src.db")
	// 本地先有一条自己的扫描（它占据 id 1，外来那条只能落在 id≥2）
	mkScanAt(t, local, 1700000000, "/mine/a", 1)
	mkScanAt(t, src, 1700000100, "/theirs/c", 2)
	image := exportImage(t, src, "ledger.db")
	if _, err := local.ImportFrom(image); err != nil {
		t.Fatal(err)
	}

	// 新扫描的 hist_id：roots 是那一条
	var newHist int64
	if err := local.db.QueryRow(
		`SELECT id FROM scan_history WHERE roots = '["/theirs/c"]'`).Scan(&newHist); err != nil {
		t.Fatalf("M352：外来的扫描没进本地: %v", err)
	}
	if newHist == 1 {
		t.Fatal("夹具前提走样：本地原有行被覆盖了")
	}

	// 每条 hist_files 的 group_id 必须属于同一 hist_id 的组
	rows, err := local.db.Query(`SELECT f.id, f.hist_id, f.group_id, g.hist_id
		FROM hist_files f LEFT JOIN hist_groups g ON g.id = f.group_id WHERE f.hist_id = ?`, newHist)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var fid, fHist, gid sql.NullInt64
		var gHist sql.NullInt64
		if err := rows.Scan(&fid, &fHist, &gid, &gHist); err != nil {
			t.Fatal(err)
		}
		n++
		if !gid.Valid || !gHist.Valid {
			t.Errorf("M352：文件行 %d 的 group_id 指向一个不存在的组（错关联）", fid.Int64)
			continue
		}
		if gHist.Int64 != newHist {
			t.Errorf("M352：文件行 %d 挂在别人的组上（group hist_id=%d want %d）",
				fid.Int64, gHist.Int64, newHist)
		}
	}
	if n != 6 {
		t.Errorf("M352：导入的 2 组 × 3 文件没搬全，实际 %d 行", n)
	}
	// CASCADE 实测生效（这条断言在替"foreign_keys 逐连接 ON"守夜，M59 同族）
	if _, err := local.db.Exec(`DELETE FROM scan_history WHERE id = ?`, newHist); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := local.db.QueryRow(`SELECT count(*) FROM hist_files WHERE hist_id = ?`, newHist).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Errorf("M352：删父行后子行剩 %d 条孤儿（外键级联没生效，导入的行是挂在坏约束上的）", left)
	}
}

// §6.3-4 op_records.hist_id 重映：源里那条 op 指向的 scan 因自然键**被跳过**时，
// 落地的 hist_id 必须指到**本地那一条**，而不是 0、也不是源库 id。
// 做成 0 的后果：记录页"联动裁剪/恢复"那条腿静默失效，界面上没有任何异常。
func TestImportRemapsOpHistIDToExistingLocal(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	src := newStoreAt(t, t.TempDir(), "src.db")
	// ★ 夹具刻意让两边的 id **不同**：本地先压一条无关扫描占住 id=1，
	//   于是"被判重跳过的那条"在本地是 2、在源库是 1。
	//   两边 id 相同时本格恒绿（照抄源 id 也等于本地 id），那是假过的夹具。
	mkScanAt(t, local, 1699000000, "/mine/filler", 1)
	localScan := mkScanAt(t, local, 1700000000, "/mine/a", 1)
	// 源里同自然键的那条（会被判重跳过）+ 一笔指向它的清理记录
	srcScan := mkScanAt(t, src, 1700000000, "/mine/a", 1)
	mkOpAt(t, src, 1700000050, "trash", "/mine/a", srcScan, 1, 1024)
	if srcScan == localScan {
		t.Fatalf("夹具前提走样：本地 %d / 源 %d 两个 id 必须不同，否则本格无法分辨重映与照抄",
			localScan, srcScan)
	}
	image := exportImage(t, src, "ledger.db")

	sum, err := local.ImportFrom(image)
	if err != nil {
		t.Fatal(err)
	}
	if sum.ScansSkipped != 1 || sum.OpsAdded != 1 {
		t.Errorf("M352：夹具的跳过/新增档不对 %+v", sum)
	}
	var gotHist int64
	var kind string
	var created int64
	if err := local.db.QueryRow(
		`SELECT hist_id, op_kind, created_at FROM op_records ORDER BY id DESC LIMIT 1`,
	).Scan(&gotHist, &kind, &created); err != nil {
		t.Fatal(err)
	}
	if gotHist != localScan {
		t.Errorf("M352：导入的清理记录 hist_id=%d，want 本地那条 %d（判重跳过的扫描也必须进映射表）",
			gotHist, localScan)
	}
	// 明细跟着落在新 op 下（不是源库 op_id）
	var items int
	if err := local.db.QueryRow(`SELECT count(*) FROM op_items`).Scan(&items); err != nil {
		t.Fatal(err)
	}
	if items != 1 {
		t.Errorf("M352：清理明细=%d want 1", items)
	}
}

// §6.3-5 孤儿：源 op 指向源库里并不存在的 scan ⇒ hist_id 置 0 且 opsOrphaned=1。
// 不猜、不硬塞（硬塞成"最新一条"会让记录页把清理挂到一次没做过的扫描上）。
func TestImportOrphanHistIDIsZeroed(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	src := newStoreAt(t, t.TempDir(), "src.db")
	mkScanAt(t, src, 1700000100, "/theirs/c", 1)
	mkOpAt(t, src, 1700000150, "trash", "/theirs/c", 999, 1, 1024) // 源库里没有 hist_id=999
	image := exportImage(t, src, "ledger.db")

	sum, err := local.ImportFrom(image)
	if err != nil {
		t.Fatal(err)
	}
	if sum.OpsOrphaned != 1 {
		t.Errorf("M352：孤儿没记账 %+v，want opsOrphaned=1", sum)
	}
	var got int64
	if err := local.db.QueryRow(`SELECT hist_id FROM op_records ORDER BY id DESC LIMIT 1`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Errorf("M352：孤儿的 hist_id=%d，want 0（不许猜一个关联）", got)
	}
}

// §6.3-6 坏文件：非账本文件 ⇒ 中文错误 + 本地逐字节不变。
// 变异：去掉事务这条仍绿，但去掉"校验在任何写之前"会红（先插父行再报错就是半截账）。
func TestImportRejectsCorruptAndKeepsLocalUntouched(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	mkScanAt(t, local, 1700000000, "/mine/a", 1)
	before := ledgerDump(t, local)

	bad := filepath.Join(t.TempDir(), "not-a-ledger.db")
	if err := os.WriteFile(bad, []byte("这不是 SQLite 文件，是一行中文说明文字"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := local.ImportFrom(bad)
	if err == nil {
		t.Fatal("M352：喂非账本文件却报告导入成功")
	}
	if !strings.Contains(err.Error(), "本地记录未改动") && !strings.Contains(err.Error(), "找不到") {
		t.Errorf("M352：错误没写明本地没动（用户会以为导入把账本搞坏了）：%v", err)
	}
	if !containsCJK(err.Error()) {
		t.Errorf("M352：错误不是中文串（界面上会是英文驱动原文）：%v", err)
	}
	if after := ledgerDump(t, local); len(after) != len(before) {
		t.Errorf("M352：坏文件改动了本地账本（before=%d after=%d）", len(before), len(after))
	} else {
		ledgerUnchanged(t, before, after)
	}
}

// 文件不存在也必须一句话能说清，不能报成"五张表整张不存在"。
// （SQL 层对不存在的文件会**建出**一个空库，所以这一格只能靠前置 stat。）
func TestImportMissingFileSaysNotFound(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	_, err := local.ImportFrom(filepath.Join(t.TempDir(), "nope.db"))
	if err == nil {
		t.Fatal("M352：不存在的文件报告导入成功")
	}
	if !strings.Contains(err.Error(), "找不到") {
		t.Errorf("M352：不存在的文件报成了结构错误：%v", err)
	}
}

// 不能把当前正在使用的账本导进自己（自己对自己合并 = 全跳过，但真出事的是中途报错把库锁住）。
func TestImportRefusesSelf(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	mkScanAt(t, local, 1700000000, "/mine/a", 1)
	_, err := local.ImportFrom(local.path)
	if err == nil || !strings.Contains(err.Error(), "当前正在使用的账本") {
		t.Errorf("M352：自导入没被挡（或文案不对）：%v", err)
	}
}

// §6.3-7 缺表：合法 SQLite 但少 op_items ⇒ 拒、点名缺哪张表、本地不动。
// 这条同时钉住"校验在任何写之前"：缺表若在 SQL 层才炸，父行可能已经插进去了。
func TestImportRejectsMissingTable(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	mkScanAt(t, local, 1700000000, "/mine/a", 1)
	before := ledgerDump(t, local)

	src := newStoreAt(t, t.TempDir(), "src.db")
	mkScanAt(t, src, 1700000100, "/theirs/c", 1)
	mkOpAt(t, src, 1700000150, "trash", "/theirs/c", 1, 1, 1024)
	image := exportImage(t, src, "ledger.db")

	// 把影像里的 op_items 抹掉，做成"合法 SQLite 但缺表"
	mut, err := sql.Open("sqlite", image)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mut.Exec(`DROP TABLE op_items`); err != nil {
		t.Fatal(err)
	}
	mut.Close()

	_, err = local.ImportFrom(image)
	if err == nil {
		t.Fatal("M352：缺 op_items 的文件报告导入成功")
	}
	if !strings.Contains(err.Error(), "op_items") {
		t.Errorf("M352：没点名缺哪张表：%v", err)
	}
	// ★ 必须认得出是**前置校验**拒的，不是 SQL 层撞出来的。
	// 变异取证：去掉 validateForeignLedger 后本条只剩驱动的 `no such table: op_items` 兜着，
	// 那句同样含 "op_items" ⇒ 上一条断言假绿；这一句把校验的身份钉回来（设计段 §6.3 变异三）。
	if !strings.Contains(err.Error(), "外来记录与本仓账本结构不符") {
		t.Errorf("M352：缺表是被 SQL 层撞出来的，不是前置校验拒的（校验没走在写之前）：%v", err)
	}
	if after := ledgerDump(t, local); len(after) != len(before) {
		t.Errorf("M352：拒绝缺表文件时已经改动了本地账本（before=%d after=%d；校验没走在写之前）",
			len(before), len(after))
	} else {
		ledgerUnchanged(t, before, after)
	}
}

// ★ 用户选中的文件**一个字都不许改**（R-2 的手动导入没有"恢复"语义，读它都不该动它）。
// 这条是设计段否掉 `history.Open(srcPath)` 做校验的直接兑现（§12 偏差一）。
func TestImportLeavesSourceFileUntouched(t *testing.T) {
	src := newStoreAt(t, t.TempDir(), "src.db")
	mkScanAt(t, src, 1700000100, "/theirs/c", 1)
	mkOpAt(t, src, 1700000150, "trash", "/theirs/c", 1, 1, 1024)
	image := exportImage(t, src, "ledger.db")

	before, err := os.ReadFile(image)
	if err != nil {
		t.Fatal(err)
	}
	local := newStoreAt(t, t.TempDir(), "local.db")
	if _, err := local.ImportFrom(image); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(image)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("M352：导入改动了用户的源文件（%d→%d 字节）——只读连接没生效",
			len(before), len(after))
	}
	// 源文件旁边也不许多出 -wal/-shm（journal_mode=delete 的单文件被开成 WAL 也是改动）
	if m, _ := filepath.Glob(image + "-*"); len(m) != 0 {
		t.Errorf("M352：导入在用户文件旁留下了附属文件 %v", m)
	}
}

// 中途失败必须整笔回滚（设计段 §6.2「要么全成要么全不动」的**可构造**形状）。
//
// 造法：外来的 hist_groups.hash 给 NULL。前置校验只看**列名在不在**，不看值，
// 所以这份文件过得了校验；而本地建表语句里 hash 是 NOT NULL ⇒ 父行进完、子行第 1 条就撞。
// 这正是"半截父行"的成因：没有事务的话，那条扫描历史会留在本地，
// 记录页显示一条展开为空的记录，且下一次导入还会把它当成"已有"跳过。
func TestImportMidChildFailureRollsBack(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	mkScanAt(t, local, 1700000000, "/mine/a", 1)
	before := ledgerDump(t, local)

	srcPath := filepath.Join(t.TempDir(), "broken_children.db")
	db, err := sql.Open("sqlite", srcPath)
	if err != nil {
		t.Fatal(err)
	}
	// 列名逐张对齐 importRequired，但 hash 不设 NOT NULL（外来文件的形状本来就不可控）
	for _, q := range []string{
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
		`INSERT INTO scan_history (saved_at, roots, filters, threads, paranoid, groups_count,
			files_count, orig_files, reclaimable, failed_json, keep_paths)
			VALUES (1700000200, '["/broken/c"]', '{}', 4, 0, 1, 1, 1, 10, '[]', '[]')`,
		`INSERT INTO hist_groups (hist_id, hash, size, reclaimable, ord) VALUES (1, NULL, 10, 10, 0)`,
		`INSERT INTO hist_files (hist_id, group_id, path, size, mtime_ns) VALUES (1, 1, '/broken/c/a.bin', 10, 1)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("夹具造失败: %v", err)
		}
	}
	db.Close()

	_, err = local.ImportFrom(srcPath)
	if err == nil {
		t.Fatal("M352：子行 NOT NULL 违约却报告导入成功（夹具没造出中途失败）")
	}
	after := ledgerDump(t, local)
	if len(after) != len(before) {
		var newKeys []string
		for k := range after {
			if _, ok := before[k]; !ok {
				newKeys = append(newKeys, k)
			}
		}
		sort.Strings(newKeys)
		t.Errorf("M352：中途失败后本地留下了半截账本（多出行 %v）；want 整笔回滚", newKeys)
	} else {
		ledgerUnchanged(t, before, after)
	}
}

func containsCJK(s string) bool {
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}
