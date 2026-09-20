package history

import (
	"testing"
)

// 2026-09-20 全面代码审查（D3）：历史载入侧的空组防御回归测试。
//
// 背景：正常写入路径有守卫——SaveScan 对 `len(g.Files) == 0` 直接 continue，
// PruneScanFiles 也会删掉成员数 <2 的组。所以空组**不可能**由本应用正常产生。
//
// 但它可以由**外部**产生：history.db 是普通 SQLite 文件，用户清理磁盘时
// 手动删过 hist_files 行、或用第三方工具改写过库、或库影像异常，都会留下
// "有 hist_groups 行、无 hist_files 行"的孤儿组。
//
// 这个孤儿组一旦被 LoadScan 原样交出，会在下游两个地方炸掉：
//
//	app.toGroupView            → 读 g.Files[0].Size，索引越界，结果页整页崩
//	ops.Execute                → keep 选取 g.Files[pickShortest(g)] 越界，
//	                             清理走到一半 panic，写前账本停在 planned，
//	                             结果集不回写（用户点了清理，应用直接消失）
//
// 载入侧是本包的职责边界，就在这里挡掉：跳过该组并计入 Failed，
// 让用户看到"有条历史里有个空组被跳过"，而不是应用崩掉。

// TestLoadScanDropsEmptyGroups 手工在库里造一个孤儿组（hist_groups 有行、
// hist_files 无行），LoadScan 必须**跳过**它、并在 Failed 里记一条可读的说明。
func TestLoadScanDropsEmptyGroups(t *testing.T) {
	s := testDB(t)

	// 先正常存一次，拿到库里的 hist_id。
	histID, err := s.SaveScan(mkScanCfg(), mkGroups(2), nil)
	if err != nil {
		t.Fatal(err)
	}

	// 模拟外部破坏：直接删掉第一个组的全部 hist_files 行，留下孤儿 hist_groups 行。
	if _, err := s.db.Exec(`DELETE FROM hist_files WHERE group_id = (
		SELECT id FROM hist_groups WHERE hist_id = ? ORDER BY id LIMIT 1)`, histID); err != nil {
		t.Fatal(err)
	}
	// 断言破坏确实生效——否则后面的用例是空转。
	var orphans int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM hist_groups g
		WHERE g.hist_id = ? AND NOT EXISTS (SELECT 1 FROM hist_files f WHERE f.group_id = g.id)`,
		histID).Scan(&orphans); err != nil {
		t.Fatal(err)
	}
	if orphans != 1 {
		t.Fatalf("构造失败：期望恰好 1 个孤儿组，实际 %d", orphans)
	}

	m, groups, err := s.LoadScan(histID)
	if err != nil {
		t.Fatalf("LoadScan 不应因空组报错（应降级为跳过）：%v", err)
	}

	// 1) 空组必须被剔除——绝不能进 groups，否则下游 toGroupView/Execute 越界。
	for _, g := range groups {
		if len(g.Files) == 0 {
			t.Fatalf("空组(GroupID=%d) 混进了结果集，下游会索引越界 panic", g.GroupID)
		}
	}
	if len(groups) != 1 {
		t.Fatalf("2 组里 1 组被破坏 → 应剩 1 组，got %d", len(groups))
	}

	// 2) 必须留下可读的失败说明，而不是静默吞掉。
	var found bool
	for _, f := range m.Failed {
		if f.Stage == "history" && f.Err != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("空组被跳过却没有任何 Failed 说明，用户会莫名少一组：%v", m.Failed)
	}
}

// TestLoadScanAllGroupsEmpty 极端情形：库影像损坏到所有组都没有文件。
// 必须返回 0 组 + 失败清单，而不是 panic 或静默返回"扫描成功但无重复"。
func TestLoadScanAllGroupsEmpty(t *testing.T) {
	s := testDB(t)
	histID, err := s.SaveScan(mkScanCfg(), mkGroups(3), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DELETE FROM hist_files WHERE group_id IN (
		SELECT id FROM hist_groups WHERE hist_id = ?)`, histID); err != nil {
		t.Fatal(err)
	}

	m, groups, err := s.LoadScan(histID)
	if err != nil {
		t.Fatalf("全空也应降级而非报错：%v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("全部组都已损坏 → 结果集必须为空，got %d 组", len(groups))
	}
	if len(m.Failed) != 3 {
		t.Fatalf("3 个空组应各记一条失败，got %d 条: %v", len(m.Failed), m.Failed)
	}
}
