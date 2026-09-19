package ops

// M3 集成测试：真实扫描（dedup pipeline）→ 保留策略 → 执行（mockTrash）→ 磁盘/结果断言。

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/dedup"
	"filededup/internal/model"
)

func TestPipelineToOpsIntegration(t *testing.T) {
	// 1) 构造已知重复结构并真实扫描
	root := t.TempDir()
	content := []byte("integrated duplicate content")
	mk := func(rel string, b []byte) string {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, b, 0o644)
		return p
	}
	a1 := mk("u/a1.bin", content)
	a2 := mk("v/b.bin", content) // 路径明显最短 → shortest 策略保留者
	a3 := mk("w/sub/a3.bin", content)
	mk("u/unique.bin", []byte("unique"))
	mk("u/other.bin", []byte("different"))

	p := dedup.New()
	groups, failed, err := p.Run(context.Background(), model.ScanConfig{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 0 || len(groups) != 1 {
		t.Fatalf("扫描结果异常: groups=%d failed=%+v", len(groups), failed)
	}

	// 2) 保留策略：shortest（b.bin 路径明显最短）
	ds, _ := ApplyKeepPolicy(groups, model.KeepPolicy{Kind: "shortest"})
	if len(ds) != 1 {
		t.Fatalf("决策数 = %d", len(ds))
	}
	keepIDs := map[uint64]bool{ds[0].KeepID: true}
	var dupIDs []uint64
	for _, g := range groups {
		for _, f := range g.Files {
			if !keepIDs[f.ID] {
				dupIDs = append(dupIDs, f.ID)
			}
		}
	}
	if len(dupIDs) != 2 {
		t.Fatalf("冗余数 = %d, want 2", len(dupIDs))
	}

	// 3) 执行 trash（mock）
	target := filepath.Join(t.TempDir(), "trashed")
	res := Execute(Options{
		Groups:  groups,
		KeepIDs: keepIDs,
		TrashFn: mockTrash(target, false),
	}, model.OpRequest{Kind: "trash", FileIDs: dupIDs})
	if len(res.OK) != 2 || len(res.Failed) != 0 {
		t.Fatalf("执行结果异常: %+v", res)
	}
	if res.Reclaimed != 2*uint64(len(content)) {
		t.Fatalf("释放空间 = %d", res.Reclaimed)
	}

	// 4) 磁盘断言：保留者存在，冗余消失，独立文件未受影响
	if _, err := os.Stat(a2); err != nil {
		t.Fatal("保留文件被删除")
	}
	for _, gone := range []string{a1, a3} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Fatalf("冗余未移除: %s", gone)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "u/unique.bin")); err != nil {
		t.Fatal("独立文件受影响")
	}
	// mock 回收站内容
	for _, n := range []string{"a1.bin", "a3.bin"} {
		if _, err := os.Stat(filepath.Join(target, n)); err != nil {
			t.Fatalf("未进入 mock 回收站: %s", n)
		}
	}
}
