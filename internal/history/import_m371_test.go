package history

// 第八轮批 3（拟 M371）P-30：孤儿计数只许落在**真的落库**的那一行上。
//
// 取证在册（§3.1e）：`import.go` 里 `sum.OpsOrphaned++` 在"源 op 指不到源库扫描"那一支，
// 而**去重跳过**（`localOps` 命中 ⇒ `OpsSkipped++` + `continue`）在它之后才发生。
// 于是同一份影像导第二次时，一行都没新增，回执却写着"发现 N 条孤儿"——
// 一份自相矛盾的收据，而 `RecordsView` 把它原样展示给用户。
//
// 夹具沿用 M352 那条孤儿格（源 op 指向源库里不存在的 hist_id=999）。

import "testing"

func TestM371SecondImportReportsNoOrphans(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	src := newStoreAt(t, t.TempDir(), "src.db")
	mkScanAt(t, src, 1700000100, "/theirs/c", 1)
	mkOpAt(t, src, 1700000150, "trash", "/theirs/c", 999, 1, 1024) // 源库里没有 hist_id=999
	image := exportImage(t, src, "ledger.db")

	first, err := local.ImportFrom(image)
	if err != nil {
		t.Fatal(err)
	}
	// ★ 前提自检：第一次**必须**记上这条孤儿，否则第二次的"零孤儿"是夹具的功劳。
	if first.OpsAdded != 1 || first.OpsOrphaned != 1 || first.ScansAdded != 1 {
		t.Fatalf("夹具前提走样：首次读数 %+v，want opsAdded=1 opsOrphaned=1 scansAdded=1", first)
	}

	second, err := local.ImportFrom(image)
	if err != nil {
		t.Fatal(err)
	}
	if second.OpsAdded != 0 || second.ScansAdded != 0 {
		t.Errorf("M371：重复导入仍新增了行 %+v", second)
	}
	if second.OpsSkipped != 1 || second.ScansSkipped != 1 {
		t.Errorf("M371：重复导入的去重没记账 %+v，want opsSkipped=1 scansSkipped=1", second)
	}
	if second.OpsOrphaned != 0 {
		t.Errorf("M371：一行都没插却报 %d 条孤儿（回执自相矛盾）：%+v", second.OpsOrphaned, second)
	}

	// 盘上读数与回执必须一致：孤儿行只有一条，hist_id 仍是 0。
	var ops, orphans int
	if err := local.db.QueryRow(`SELECT count(*) FROM op_records`).Scan(&ops); err != nil {
		t.Fatal(err)
	}
	if err := local.db.QueryRow(`SELECT count(*) FROM op_records WHERE hist_id=0`).Scan(&orphans); err != nil {
		t.Fatal(err)
	}
	if ops != 1 || orphans != 1 {
		t.Errorf("M371：重复导入改变了盘上账本 ops=%d orphans=%d, want 1/1", ops, orphans)
	}
}
