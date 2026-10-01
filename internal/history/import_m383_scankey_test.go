package history

// 第九轮修订批 B2（拟 M383，裁定 2026-10-01 裁-3「键剔除三列派生值」）。
//
// 缺陷形状（报告 P1-4）：`scanKey` 装着 groups_count / files_count / reclaimable 三列，
// 而这三列会被本仓自己的 `PruneScanFiles` 在**一条事务里重写**（清理之后组数、文件数、
// 可回收量都变小）。于是"同一份判断被修剪过"＝"键变了"＝**判成新扫描**：
// 重导一次同一份影像，本地多出一行、并把最旧那条按 M356 的裁剪淘汰掉——
// 用户看到的是"清理后重导，记录反而多了几条、旧的没了"。
//
// ★ 走真实导出件（exportImage），理由沿用 import_m352_test.go 开头那一段。
// ★ 改前必红的实测方式见 04 §6.70（撤键的临时变异 MU9-g）：这一批的判据全部落在
//   导入回执与盘上行数上，不看键的实现细节。

import (
	"path/filepath"
	"testing"
)

// P-51：修剪过的本地那条，再导入**同一份**影像必须仍判重复、五表一字不动。
func TestM383PrunedRowStillMatchesSameImage(t *testing.T) {
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

	// 对本地这条做一次修剪：删掉第二个组的全部成员 ⇒ groups_count/files_count/reclaimable
	// 三列在同一条事务里被重写（这正是"派生值"四个字的形状）。
	var localID int64
	if err := local.db.QueryRow(`SELECT id FROM scan_history ORDER BY id ASC LIMIT 1`).Scan(&localID); err != nil {
		t.Fatalf("读不到本地那条：%v", err)
	}
	gone := map[string]bool{}
	for j := 0; j < 3; j++ {
		gone[filepath.Join("/theirs/c", "b", string(rune('0'+j))+".bin")] = true
	}
	if err := local.PruneScanFiles(localID, gone); err != nil {
		t.Fatalf("M383：夹具的修剪失败了：%v", err)
	}

	// 前提自检：修剪确实改写了那三列（否则"仍判重复"是夹具没动过东西的假绿）
	var g, f int64
	var r int64
	if err := local.db.QueryRow(`SELECT groups_count, files_count, reclaimable FROM scan_history WHERE id = ?`,
		localID).Scan(&g, &f, &r); err != nil {
		t.Fatalf("读修剪后的三列失败：%v", err)
	}
	if g == 2 || f == 6 || r == sr {
		t.Fatalf("前提自检失败：修剪后三列仍是 %d/%d/%d —— 影像与本地没差异，本判据无法归因给键", g, f, r)
	}

	before := ledgerDump(t, local)
	sum, err := local.ImportFrom(image)
	if err != nil {
		t.Fatalf("二导失败：%v", err)
	}
	after := ledgerDump(t, local)
	if sum.ScansAdded != 0 || sum.ScansSkipped != 1 {
		t.Fatalf("M383：修剪过的同一条判断被认成新扫描（回执 %+v）—— 三列派生值还在键里，"+
			"而它们会被 PruneScanFiles 重写", sum)
	}
	ledgerUnchanged(t, before, after)
	var rows int
	if err := local.db.QueryRow(`SELECT COUNT(*) FROM scan_history`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("M383：重导一次同一份影像后本地有 %d 条扫描（应仍是 1 条）", rows)
	}
}

// P-51 相邻格：`reclaimable` 单列不同 ⇒ 现在必须**判重复**（裁-3 之后期望反转的那一档）。
//
// ★ 这一格原先在 import_m377_test.go 的 P-40 里，期望是"认成新行"；2026-10-01 裁-3 判定
//
//	可回收量是派生值，故把它从 P-40 移出、期望改写在此。不是删掉，也不是假绿。
func TestM383ReclaimableOnlyDiffersIsDuplicate(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	src := newStoreAt(t, t.TempDir(), "src.db")
	st, sp, so, sr := baseParams()
	mkScanParams(t, local, 1700000000, "/mine/a", 2, st, sp, so, sr)
	mkScanParams(t, src, 1700000000, "/mine/a", 2, st, sp, so, sr+999) // ★ 只差 reclaimable
	image := exportImage(t, src, "image.db")

	before := ledgerDump(t, local)
	sum, err := local.ImportFrom(image)
	if err != nil {
		t.Fatalf("导入失败：%v", err)
	}
	if sum.ScansAdded != 0 || sum.ScansSkipped != 1 {
		t.Fatalf("M383：只差 reclaimable 被判成新扫描（回执 %+v）—— 可回收量会被修剪重写，不进键", sum)
	}
	ledgerUnchanged(t, before, ledgerDump(t, local))
}

// P-52 负控制（本批的"半边没被顺手拆掉"）：groups_count / files_count 这两列**单独**变一变，
// 同样必须判重复——它们与 reclaimable 同属派生三列；而 threads / orig_files 那一半
// 仍须认成新行，那一格由 import_m377_test.go 的 P-40（本批改名后只剩两档）继续钉住，
// 本批**不另立重复判据**（两处措辞必然分叉，处置同第八轮删 TestM359SnapshotFileModeIsOwnerOnly）。
func TestM383DerivedCountsOnlyDifferAreDuplicate(t *testing.T) {
	for _, tc := range []struct {
		name   string
		update string // 只改派生列，六字段键一字不动
	}{
		{"groups_count", `UPDATE scan_history SET groups_count = 9`},
		{"files_count", `UPDATE scan_history SET files_count = 27`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			local := newStoreAt(t, t.TempDir(), "local.db")
			src := newStoreAt(t, t.TempDir(), "src.db")
			st, sp, so, sr := baseParams()
			mkScanParams(t, local, 1700000000, "/mine/a", 2, st, sp, so, sr)
			mkScanParams(t, src, 1700000000, "/mine/a", 2, st, sp, so, sr)
			if _, err := src.db.Exec(tc.update); err != nil {
				t.Fatal(err)
			}
			image := exportImage(t, src, "image.db")

			before := ledgerDump(t, local)
			sum, err := local.ImportFrom(image)
			if err != nil {
				t.Fatalf("导入失败：%v", err)
			}
			if sum.ScansAdded != 0 || sum.ScansSkipped != 1 {
				t.Fatalf("M383：%s 单列不同被判成新扫描（回执 %+v）", tc.name, sum)
			}
			ledgerUnchanged(t, before, ledgerDump(t, local))
		})
	}
}
