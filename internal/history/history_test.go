package history

import (
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/model"
)

// testDB 在 t.TempDir 下建库。
func testDB(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mkScanCfg() model.ScanConfig {
	return model.ScanConfig{
		Roots:    []string{"/a", "/b"},
		Filters:  model.Filters{MinSize: 1024, IncludeExts: []string{".jpg"}},
		Threads:  4,
		Paranoid: true,
	}
}

func mkGroups(n int) []*model.DuplicateGroup {
	var out []*model.DuplicateGroup
	id := uint64(100)
	for i := 0; i < n; i++ {
		var fs []*model.FileEntry
		for j := 0; j < 3; j++ {
			id++
			fs = append(fs, &model.FileEntry{ID: id,
				Path: filepath.Join("/root", string(rune('a'+i)), string(rune('0'+j))+".bin"),
				Size: uint64(1000 + i), ModTime: int64(1234567 + j)})
		}
		var h [32]byte
		h[0] = byte(i)
		out = append(out, &model.DuplicateGroup{
			GroupID: uint64(i + 1), Files: fs,
			Reclaimable: 2 * uint64(1000+i), Hash: h})
	}
	return out
}

func TestOpenAndReopen(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "history.db")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveScan(mkScanCfg(), mkGroups(2), nil); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s2, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	ms, err := s2.ListScans()
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("重开后应仍有 1 条: %d", len(ms))
	}
}

func TestCorruptRebuild(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "history.db")
	if err := os.WriteFile(p, []byte("not a sqlite file at all...."), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(p) // 损坏自愈：删除重建
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ms, err := s.ListScans()
	if err != nil || len(ms) != 0 {
		t.Fatalf("重建后应为空库: %v %v", ms, err)
	}
	if _, err := s.SaveScan(mkScanCfg(), mkGroups(1), nil); err != nil {
		t.Fatalf("重建后应可写: %v", err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	s := testDB(t)
	groups := mkGroups(2)
	failed := []model.FailedItem{{Path: "/x/y.bin", Stage: "hash", Err: "io"}}
	histID, err := s.SaveScan(mkScanCfg(), groups, failed)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateKeepPaths(histID, []string{groups[0].Files[0].Path}); err != nil {
		t.Fatal(err)
	}
	meta, loaded, err := s.LoadScan(histID)
	if err != nil {
		t.Fatal(err)
	}
	if meta.ID != histID || meta.Threads != 4 || !meta.Paranoid {
		t.Fatalf("meta 字段失真: %+v", meta)
	}
	if len(meta.Roots) != 2 || meta.Roots[0] != "/a" {
		t.Fatalf("roots 失真: %+v", meta.Roots)
	}
	if meta.Filters.MinSize != 1024 || meta.Filters.IncludeExts[0] != ".jpg" {
		t.Fatalf("filters 失真: %+v", meta.Filters)
	}
	if len(meta.Failed) != 1 || meta.Failed[0].Stage != "hash" {
		t.Fatalf("failed 失真: %+v", meta.Failed)
	}
	if len(meta.KeepPaths) != 1 || meta.KeepPaths[0] != groups[0].Files[0].Path {
		t.Fatalf("keepPaths 失真: %+v", meta.KeepPaths)
	}
	if meta.Groups != 2 || meta.Files != 6 || meta.OrigFiles != 6 || meta.Reclaimable != 4002 {
		t.Fatalf("计数失真: %+v", meta)
	}
	if len(loaded) != 2 {
		t.Fatalf("组数失真: %d", len(loaded))
	}
	var seenIDs = map[uint64]bool{}
	for i, g := range loaded {
		if len(g.Files) != 3 {
			t.Fatalf("组 %d 文件数失真: %d", i, len(g.Files))
		}
		wantHash := groups[i].Hash
		if g.Hash != wantHash {
			t.Fatalf("组 %d hash 失真", i)
		}
		for j, f := range g.Files {
			if f.Path != groups[i].Files[j].Path || f.Size != groups[i].Files[j].Size ||
				f.ModTime != groups[i].Files[j].ModTime {
				t.Fatalf("文件失真: %+v vs %+v", f, groups[i].Files[j])
			}
			if seenIDs[f.ID] {
				t.Fatalf("文件 ID 重复: %d", f.ID)
			}
			seenIDs[f.ID] = true
		}
	}
}

func TestListScansDesc(t *testing.T) {
	s := testDB(t)
	var ids []int64
	for i := 0; i < 3; i++ {
		id, err := s.SaveScan(mkScanCfg(), mkGroups(1), nil)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	ms, err := s.ListScans()
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 3 || ms[0].ID != ids[2] || ms[2].ID != ids[0] {
		t.Fatalf("应时间降序: %+v", ms)
	}
}

func TestEvictOldest(t *testing.T) {
	s := testDB(t)
	var first int64
	for i := 0; i <= MaxScanHistory; i++ { // 21 条 → 淘汰 1 条
		cfg := mkScanCfg()
		cfg.Roots = []string{string(rune('A' + i))}
		id, err := s.SaveScan(cfg, mkGroups(1), nil)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = id
		}
	}
	ms, err := s.ListScans()
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != MaxScanHistory {
		t.Fatalf("上限 %d，实得 %d", MaxScanHistory, len(ms))
	}
	for _, m := range ms {
		if m.ID == first {
			t.Fatal("最旧记录未被淘汰")
		}
	}
	// 级联：被淘汰记录的组/文件行必须清空（孤儿会让 LoadScan(id) 拿到空组而非报错，
	// 直接断言子表计数）
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM hist_files WHERE hist_id = ?`, first).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("级联删除失效: 孤儿文件行 %d", n)
	}
}

func TestPruneScanFiles(t *testing.T) {
	s := testDB(t)
	// 组1: a0 a1 a2；组2: b0 b1 b2（mkGroups 结构）
	groups := mkGroups(2)
	histID, err := s.SaveScan(mkScanCfg(), groups, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateKeepPaths(histID, []string{groups[0].Files[0].Path, groups[1].Files[0].Path}); err != nil {
		t.Fatal(err)
	}
	// 删组2 全部 b* → 组2 消失；组1 不动
	gone := map[string]bool{
		groups[1].Files[0].Path: true,
		groups[1].Files[1].Path: true,
		groups[1].Files[2].Path: true,
	}
	if err := s.PruneScanFiles(histID, gone); err != nil {
		t.Fatal(err)
	}
	meta, loaded, err := s.LoadScan(histID)
	if err != nil {
		t.Fatal(err)
	}
	// 存活组 a：3 文件 × 1000B → reclaimable 2*1000 = 2000；orig_files 记录原始数不变
	if meta.Groups != 1 || meta.Files != 3 || meta.OrigFiles != 6 || meta.Reclaimable != 2000 {
		t.Fatalf("裁剪计数失真: %+v", meta)
	}
	if len(loaded) != 1 || len(loaded[0].Files) != 3 {
		t.Fatalf("裁剪组失真: %+v", loaded)
	}
	// keep_paths 中被裁掉的 b0 应移除，存活的 a0 保留
	if len(meta.KeepPaths) != 1 || meta.KeepPaths[0] != groups[0].Files[0].Path {
		t.Fatalf("keep_paths 未裁剪: %+v", meta.KeepPaths)
	}
}

func TestDeleteAndClear(t *testing.T) {
	s := testDB(t)
	id1, _ := s.SaveScan(mkScanCfg(), mkGroups(1), nil)
	if _, err := s.SaveScan(mkScanCfg(), mkGroups(1), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteScan(id1); err != nil {
		t.Fatal(err)
	}
	ms, _ := s.ListScans()
	if len(ms) != 1 {
		t.Fatalf("删除失败: %d", len(ms))
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM hist_groups WHERE hist_id = ?`, id1).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("删除未级联: %d", n)
	}
	if err := s.ClearScans(); err != nil {
		t.Fatal(err)
	}
	ms, _ = s.ListScans()
	if len(ms) != 0 {
		t.Fatal("清空失败")
	}
	if _, _, err := s.LoadScan(id1); err == nil {
		t.Fatal("不存在的记录应报错")
	}
}

func TestSchemaTablesAndForeignKeys(t *testing.T) {
	s := testDB(t)
	// op 表在 Task 4 建齐（Task 7 只加 API）
	for _, tbl := range []string{"scan_history", "hist_groups", "hist_files", "op_records", "op_items"} {
		var name string
		err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name = ?`, tbl).Scan(&name)
		if err != nil || name != tbl {
			t.Fatalf("缺表 %s: %v", tbl, err)
		}
	}
	var fk int
	if err := s.db.QueryRow(`SELECT * FROM pragma_foreign_keys()`).Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("foreign_keys 应开启: %v %v", fk, err)
	}
	var ver int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&ver); err != nil || ver != SchemaVersion {
		t.Fatalf("user_version=%d err=%v", ver, err)
	}
}
