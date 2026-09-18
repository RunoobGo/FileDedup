package dedup

// P0-2 回归：增量缓存不得改变分组资格——同一数据集，
// "一次全量扫描"与"分多轮增量扫描（带缓存）"必须产出一致的重复组。

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"filededup/internal/cache"
	"filededup/internal/model"
)

func lgPayload(seed byte, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i%251) + seed
	}
	return b
}

func joinComma(s []string) string {
	r := ""
	for i, v := range s {
		if i > 0 {
			r += ","
		}
		r += v
	}
	return r
}

// largeFileDupAcrossIncrementalScans：三个同内容大文件分三轮进入扫描根，
// 每轮都带缓存。修正前第 3 轮只能报出 2 个（C 与已缓存的 A/B 永远不同桶）。
func TestLargeFileDupAcrossIncrementalScans(t *testing.T) {
	root := t.TempDir()
	cch, err := cache.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cch.Close()
	payload := lgPayload(11, 300*1024) // > SmallFileMax，走两阶段
	cfg := model.ScanConfig{Roots: []string{root}, UseCache: true, Threads: 2}

	write := func(name string) {
		if err := os.WriteFile(filepath.Join(root, name), payload, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	countAll := func() int {
		g, _, err := New().WithCache(cch).Run(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, gg := range g {
			n += len(gg.Files)
		}
		return n
	}

	write("A.bin")
	if n := countAll(); n != 0 {
		t.Fatalf("第 1 轮（仅 A）应无重复, got %d", n)
	}
	write("B.bin")
	if n := countAll(); n != 2 {
		t.Fatalf("第 2 轮（A,B）应报 2 个, got %d", n)
	}
	write("C.bin")
	if n := countAll(); n != 3 {
		t.Fatalf("P0-2 漏报：第 3 轮（A,B,C）应报 3 个, got %d", n)
	}
	// 第 4 轮：全命中缓存，结果必须稳定
	if n := countAll(); n != 3 {
		t.Fatalf("第 4 轮（全命中）应报 3 个, got %d", n)
	}
}

// smallFileDupAcrossIncrementalScans：小文件路径同样验证（一趟双哈希 + 缓存 full）。
func TestSmallFileDupAcrossIncrementalScans(t *testing.T) {
	root := t.TempDir()
	cch, _ := cache.Open(filepath.Join(t.TempDir(), "c.db"))
	defer cch.Close()
	payload := lgPayload(5, 4096) // << SmallFileMax
	cfg := model.ScanConfig{Roots: []string{root}, UseCache: true, Threads: 2}
	run := func(want int) {
		t.Helper()
		g, _, err := New().WithCache(cch).Run(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, gg := range g {
			n += len(gg.Files)
		}
		if n != want {
			t.Fatalf("小文件增量: got %d want %d", n, want)
		}
	}
	os.WriteFile(filepath.Join(root, "a.bin"), payload, 0o644)
	run(0)
	os.WriteFile(filepath.Join(root, "b.bin"), payload, 0o644)
	run(2)
	os.WriteFile(filepath.Join(root, "c.bin"), payload, 0o644)
	run(3)
}

// 核心不变量：同一数据集 "分轮增量（带缓存）" 与 "一次全量（无缓存）" 结果一致。
// 这条测试同时锁住 P0-1（可重复扫描）与 P0-2（缓存不改变分组资格）。
func TestIncrementalEqualsFullScan(t *testing.T) {
	root := t.TempDir()
	// 混合数据集：2 个大文件重复组 + 1 个小文件重复组 + 若干独立文件
	os.WriteFile(filepath.Join(root, "L1.bin"), lgPayload(21, 260*1024), 0o644)
	os.WriteFile(filepath.Join(root, "L2.bin"), lgPayload(21, 260*1024), 0o644)
	os.WriteFile(filepath.Join(root, "S1.bin"), lgPayload(22, 3000), 0o644)
	os.WriteFile(filepath.Join(root, "S2.bin"), lgPayload(22, 3000), 0o644)
	os.WriteFile(filepath.Join(root, "solo.bin"), lgPayload(23, 100*1024), 0o644)

	// 基线：无缓存一次全量
	gBase, fBase, err := New().Run(context.Background(), model.ScanConfig{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	if len(gBase) != 2 {
		t.Fatalf("基线应 2 组, got %d failed=%+v", len(gBase), fBase)
	}

	// 增量：逐文件添加 + 每轮带缓存（复用同一 Pipeline，验证 P0-1 复位）
	cch, _ := cache.Open(filepath.Join(t.TempDir(), "c.db"))
	defer cch.Close()
	p := New().WithCache(cch)
	cfg := model.ScanConfig{Roots: []string{root}, UseCache: true}
	if _, _, err := p.Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	// 改 S1 内容并新增 L3（与 L1/L2 同内容），再次扫描（第二次 Run → 验证终态复位）
	os.WriteFile(filepath.Join(root, "S1.bin"), lgPayload(24, 3000), 0o644)
	os.WriteFile(filepath.Join(root, "L3.bin"), lgPayload(21, 260*1024), 0o644)
	gInc, _, err := p.Run(context.Background(), cfg) // 修正前这里直接报"非法状态转换"
	if err != nil {
		t.Fatalf("P0-1: 第二次 Run 被拒: %v", err)
	}
	// 期望：L 组 = {L1,L2,L3} 3 个；S 组因 S1 改写只剩 S2 孤身 → 无 S 组
	if len(gInc) != 1 || len(gInc[0].Files) != 3 {
		t.Fatalf("P0-2: 增量结果与预期不符 groups=%d", len(gInc))
	}
	names := []string{}
	for _, f := range gInc[0].Files {
		names = append(names, filepath.Base(f.Path))
	}
	sort.Strings(names)
	if joinComma(names) != "L1.bin,L2.bin,L3.bin" {
		t.Fatalf("组成员错误: %v", names)
	}
}
