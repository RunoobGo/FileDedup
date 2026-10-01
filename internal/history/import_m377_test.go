package history

// M377（第八轮裁定回收 / 设计段 §2.2）：`scanKey` 补齐四字段（裁定 M375 取向①）。
//
// 缺陷形状（spec §3.2 末段登记的那一格）：自然键只有
// `saved_at + roots + filters + groups_count + files_count`，不含 `threads / paranoid /
// orig_files / reclaimable` ⇒ 外来影像里"同五字段、但另一套参数"的扫描被判成重复，
// **整条连子行一起抑制**，并把它的 ops 关联到本地那条**另一套参数**跑出来的扫描上。
// 判成重复看起来无害，实际是把两份不同的判断合并成一份——而 `paranoid` 决定分组可信度、
// `orig_files`/`reclaimable` 是读数本身。
//
// ★ 全部走真实导出件（exportImage），沿用 import_m352_test.go 开头的理由：
//   手搓源库会让"导出的东西导不回来"这一格永远绿着。
// ★ P-41/P-42 两条负控制与 P-39 同批存在：把判重改严很容易一路改成"永远不判重"，
//   那是另一种数据丢失（导两次翻一倍）。
//
// ★ 2026-10-01 第九轮批 B2（裁-3，M383）改了本文件的一条期望：键里**去掉**
//   groups_count / files_count / reclaimable 三列（它们会被 PruneScanFiles 重写），
//   于是上面那句"四列补齐"只剩 **threads / paranoid / orig_files** 三列，
//   而 P-40 的 `reclaimable` 那一档期望反转。判据本体与反转后的那一格在
//   `import_m383_scankey_test.go`。本文件其余格子（P-39/P-41/P-42）不受影响。

import (
	"path/filepath"
	"testing"
)

// mkScanParams 落一条扫描历史，四个参数列（threads/paranoid/orig_files/reclaimable）逐个可控，
// 而五字段键（saved_at/roots/filters/groups_count/files_count）由 seed 固定派生。
// ★ 刻意不复用 mkScanAt：那把尺子里 threads/paranoid/orig_files/reclaimable 是写死的，
//
//	而本批的判据恰好就是"这四列各自单独变一变"。
func mkScanParams(t *testing.T, s *Store, savedAt int64, root string, groups int,
	threads int64, paranoid bool, origFiles, reclaimable int64) int64 {
	t.Helper()
	rootsJS := `["` + root + `"]`
	filtersJS := `{"minSize":1024,"includeExts":[]}`
	files := int64(groups * 3)
	res, err := s.db.Exec(`INSERT INTO scan_history
		(saved_at, roots, filters, threads, paranoid, groups_count, files_count, orig_files, reclaimable, failed_json, keep_paths)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '[]', '[]')`,
		savedAt, rootsJS, filtersJS, threads, paranoid, groups, files, origFiles, reclaimable)
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

// baseParams 是本批四档对照的公共底版：五字段与四参数全部相同的一条扫描。
func baseParams() (threads int64, paranoid bool, origFiles, reclaimable int64) {
	return 4, false, 6, 4001
}

// TestM377DifferentParanoidIsNotADuplicate 是 P-39 本体（改前必红：改前 ScansSkipped=1）。
// 判据三半：① 认成新行；② 子行搬运到位（不是插一条空扫描）；③ 外来 ops 挂到**新行**上，
// 而不是挂到本地那条另一套参数跑出来的扫描上。
func TestM377DifferentParanoidIsNotADuplicate(t *testing.T) {
	dir := t.TempDir()
	local := newStoreAt(t, dir, "local.db")
	src := newStoreAt(t, t.TempDir(), "src.db")

	lt, lp, lo, lr := baseParams()
	mkScanParams(t, local, 1700000000, "/mine/a", 2, lt, lp, lo, lr)

	st, sp, so, sr := baseParams()
	sp = true // ★ 唯一差异：paranoid
	srcID := mkScanParams(t, src, 1700000000, "/mine/a", 2, st, sp, so, sr)
	mkOpAt(t, src, 1700000050, "quarantine", "/trash/x", srcID, 1, 1024)
	image := exportImage(t, src, "image.db")

	sum, err := local.ImportFrom(image)
	if err != nil {
		t.Fatalf("导入失败：%v", err)
	}
	if sum.ScansAdded != 1 || sum.ScansSkipped != 0 {
		t.Fatalf("M377：参数不同的扫描不得判成重复，回执失真 %+v（want added=1 skipped=0）", sum)
	}
	scans, err := local.ListScans()
	if err != nil {
		t.Fatal(err)
	}
	if len(scans) != 2 {
		t.Fatalf("M377：本地应有 2 条（1 原有 + 1 新增），实得 %d", len(scans))
	}

	// ② 新增的那条必须带子行（改前这一格压根不存在，因为它被整条抑制了）
	var newID, oldID int64
	for _, m := range scans {
		if m.Paranoid {
			newID = m.ID
		} else {
			oldID = m.ID
		}
	}
	if newID == 0 || oldID == 0 {
		t.Fatalf("M377：两条扫描没按 paranoid 分开（%+v）", scans)
	}
	_, groups, err := local.LoadScan(newID)
	if err != nil {
		t.Fatalf("读新增扫描失败：%v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("M377：新增扫描的子行没搬到位（组数 %d，应为 2；插一条空行等于没搬）", len(groups))
	}

	// ③ ops 的 hist_id 必须指向新行
	var opHist int64
	var opID int64
	if err := local.db.QueryRow(`SELECT id FROM op_records ORDER BY id DESC LIMIT 1`).Scan(&opID); err != nil {
		t.Fatalf("读不到导入的清理记录：%v", err)
	}
	if err := local.db.QueryRow(`SELECT hist_id FROM op_records WHERE id = ?`, opID).Scan(&opHist); err != nil {
		t.Fatalf("读 ops 的 hist_id 失败：%v", err)
	}
	if opHist != newID {
		t.Fatalf("M377：外来 ops 挂到了 hist_id=%d，应挂到新插的那条（%d）——挂在旧行上就是把两份不同的判断合并成一份", opHist, newID)
	}
	if opHist == oldID {
		t.Fatal("M377：外来 ops 正是挂到了本地那条 paranoid 不同的扫描上（本批要修的缺陷形状）")
	}
}

// TestM377OtherTwoParamsEachBreakDedup 是 P-40：另两档各单独变一变，两条都必须认成新行。
// ★ 一格一参：只补 paranoid 而漏掉另几列是本批最容易交出去的"半修"（变异 MU-x 钉它）。
//
// ★ 这一档原有**三条**（threads / orig_files / **reclaimable**）。2026-10-01 裁-3 把
//
//	`reclaimable` 判定为**派生值**（`PruneScanFiles` 会在一条事务里重写
//	groups_count / files_count / reclaimable），它出键 ⇒ 此档期望**反转**为"仍判重复"，
//	改写并搬到 `import_m383_scankey_test.go` 的 P-51 相邻格。
//	★ 这不是"测试本来就红"，也不是为了让门禁变绿而改断言：**裁定直接改写了契约**，
//	测试跟着新契约走；旧契约那半边（threads/orig_files 仍须认成新行）由下面两格继续钉住，
//	并另立负控制 P-52（见 P-42 那格的处置）。
func TestM377OtherTwoParamsEachBreakDedup(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t int64, p bool, o, r int64) (int64, bool, int64, int64)
	}{
		{"threads", func(t int64, p bool, o, r int64) (int64, bool, int64, int64) { return t + 4, p, o, r }},
		{"orig_files", func(t int64, p bool, o, r int64) (int64, bool, int64, int64) { return t, p, o + 7, r }},
	}
	lt, lp, lo, lr := baseParams()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			local := newStoreAt(t, t.TempDir(), "local.db")
			src := newStoreAt(t, t.TempDir(), "src.db")
			mkScanParams(t, local, 1700000000, "/mine/a", 2, lt, lp, lo, lr)
			st, sp, so, sr := tc.mutate(lt, lp, lo, lr)
			mkScanParams(t, src, 1700000000, "/mine/a", 2, st, sp, so, sr)
			image := exportImage(t, src, "image.db")

			sum, err := local.ImportFrom(image)
			if err != nil {
				t.Fatalf("导入失败：%v", err)
			}
			if sum.ScansAdded != 1 || sum.ScansSkipped != 0 {
				t.Fatalf("M377：%s 单列不同仍被判重复，回执 %+v", tc.name, sum)
			}
		})
	}
}

// TestM377IdenticalParamsStillDedup 是 P-41 负控制（防判重判死，改前即绿）：
// 参数完全相同的同一份文件**连导两次**，第二次必须整条跳过、五表一字不动。
func TestM377IdenticalParamsStillDedup(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	src := newStoreAt(t, t.TempDir(), "src.db")
	st, sp, so, sr := baseParams()
	mkScanParams(t, src, 1700000000, "/theirs/c", 2, st, sp, so, sr)
	image := exportImage(t, src, "image.db")

	if sum, err := local.ImportFrom(image); err != nil {
		t.Fatalf("首导失败：%v", err)
	} else if sum.ScansAdded != 1 {
		t.Fatalf("首导应插入 1 条，回执 %+v", sum)
	}
	before := ledgerDump(t, local)
	sum, err := local.ImportFrom(image)
	if err != nil {
		t.Fatalf("二导失败：%v", err)
	}
	after := ledgerDump(t, local)
	if sum.ScansAdded != 0 || sum.ScansSkipped != 1 {
		t.Fatalf("M377：参数完全相同仍判重失败（导两次翻一倍就是另一种数据丢失），回执 %+v", sum)
	}
	ledgerUnchanged(t, before, after)
}

// TestM377SameKeyInsideOneImageStillDedup 是 P-42 负控制（改前即绿）：
// 同一份外来文件**内部**有两条自然键完全相同的扫描 ⇒ 第二条仍认成重复
// （import.go:410 那行 `localScans[k] = histID` 的行为不许被本批改掉）。
func TestM377SameKeyInsideOneImageStillDedup(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	src := newStoreAt(t, t.TempDir(), "src.db")
	st, sp, so, sr := baseParams()
	mkScanParams(t, src, 1700000000, "/theirs/c", 2, st, sp, so, sr)
	mkScanParams(t, src, 1700000000, "/theirs/c", 2, st, sp, so, sr) // ★ 与上一条逐字段相同
	image := exportImage(t, src, "image.db")

	sum, err := local.ImportFrom(image)
	if err != nil {
		t.Fatalf("导入失败：%v", err)
	}
	if sum.ScansAdded != 1 || sum.ScansSkipped != 1 {
		t.Fatalf("M377：文件内部同键的两条应插一跳一，回执 %+v", sum)
	}
	scans, err := local.ListScans()
	if err != nil {
		t.Fatal(err)
	}
	if len(scans) != 1 {
		t.Fatalf("M377：本地应只有 1 条，实得 %d", len(scans))
	}
}
