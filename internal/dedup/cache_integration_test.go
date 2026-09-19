package dedup

// M4 集成：缓存命中二次扫描零读盘 + 结果一致性 + 修改后失效。

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"filededup/internal/cache"
	"filededup/internal/model"
)

func TestCacheSecondScan(t *testing.T) {
	// 数据：3 组重复（大文件 200KB + 小文件 + 大文件2），含独立文件
	root := t.TempDir()
	write := func(rel string, b []byte) string {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, b, 0o644)
		return p
	}
	big := make([]byte, 200<<10) // >128KiB 走大文件路径（预筛+全量两阶段）
	for i := range big {
		big[i] = byte(i * 7)
	}
	small := []byte("small duplicate content")
	write("g1/a.bin", big)
	write("g1/b.bin", big)
	write("g2/s1.txt", small)
	write("g2/s2.txt", small)
	write("u/uniq.bin", []byte("unique content here"))

	cch, err := cache.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cch.Close()

	cfg := model.ScanConfig{Roots: []string{root}, UseCache: true}

	// 首扫：无缓存
	p1 := New().WithCache(cch)
	g1, failed1, err := p1.Run(context.Background(), cfg)
	if err != nil || len(failed1) != 0 || len(g1) != 2 {
		t.Fatalf("首扫: groups=%d failed=%+v err=%v", len(g1), failed1, err)
	}

	// 缓存应有条目（候选文件：4 个重复文件；uniq 独立 size 被淘汰不缓存）
	st, _ := cch.GetStats()
	if st.Entries != 4 || st.WithFull != 4 {
		t.Fatalf("缓存条目 = %+v, want entries=4 withFull=4", st)
	}

	// 二扫：全部命中，结果一致
	p2 := New().WithCache(cch)
	g2, failed2, err := p2.Run(context.Background(), cfg)
	if err != nil || len(failed2) != 0 || len(g2) != 2 {
		t.Fatalf("二扫: groups=%d failed=%+v err=%v", len(g2), failed2, err)
	}
	if g1[0].Reclaimable != g2[0].Reclaimable || g1[1].Reclaimable != g2[1].Reclaimable {
		t.Fatalf("两次扫描结果不一致: %d/%d vs %d/%d",
			g1[0].Reclaimable, g1[1].Reclaimable, g2[0].Reclaimable, g2[1].Reclaimable)
	}
	if g1[0].Hash != g2[0].Hash {
		t.Fatal("组哈希不一致（缓存组装错误）")
	}

	// 修改其中一个副本（内容变、size 同、mtime 变）→ 三扫该组消失
	s2p := filepath.Join(root, "g2/s2.txt")
	os.WriteFile(s2p, []byte("small duplicate contentX"), 0o644) // size+1
	p3 := New().WithCache(cch)
	g3, _, err := p3.Run(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(g3) != 1 {
		t.Fatalf("修改后组数 = %d, want 1", len(g3))
	}
}

// TestCacheMtimeContentChanged C1 回归：文件内容变更但 size+mtime 不变时，
// 缓存命中绝不可让"旧 full 哈希"把内容已变文件误判进重复组（否则会误删）。
//
// 触发条件：首扫缓存 x/y（内容 A，互为重复）。二扫前把 x 内容改为 B（同长度），
// 并把 mtime 拨回原值 → 缓存仍以 (path,size,mtime) 命中，但内容已变。
//
//	修复前：命中分支直接采用缓存的 Head/Tail/Full（内容 A）→ x 与 y 同桶 → 误报重复组。
//	修复后：命中后再算实际抽样(head/tail)，与缓存抽样不一致 → 不信任缓存 full，
//	        x 以实际抽样(contentB)分桶、阶段3重算 full → 与 y(contentA)不同 → 无重复组。
func TestCacheMtimeContentChanged(t *testing.T) {
	root := t.TempDir()
	write := func(rel string, b []byte) string {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, b, 0o644)
		return p
	}
	// 大文件（>128KiB 走两阶段：预筛采样 + 阶段3全量），内容确定性。
	n := 200 << 10
	contentA := make([]byte, n)
	contentB := make([]byte, n)
	for i := range contentA {
		contentA[i] = byte(i)
		contentB[i] = byte(i * 3) // 同长度、不同内容 → head/tail 抽样必然不同
	}
	x := write("x.bin", contentA)
	_ = write("y.bin", contentA) // 与 x 重复

	cch, err := cache.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cch.Close()

	cfg := model.ScanConfig{Roots: []string{root}, UseCache: true}

	// 首扫：建立缓存（x,y 应判为 1 个重复组）
	p1 := New().WithCache(cch)
	g1, failed1, err := p1.Run(context.Background(), cfg)
	if err != nil || len(failed1) != 0 {
		t.Fatalf("首扫: failed=%+v err=%v", failed1, err)
	}
	if len(g1) != 1 {
		t.Fatalf("首扫组数 = %d, want 1", len(g1))
	}

	// 抓取 x 的原始 mtime（缓存键含 mtime）。首扫后未再写入，故与缓存一致。
	fi, err := os.Stat(x)
	if err != nil {
		t.Fatal(err)
	}
	origMtime := fi.ModTime()

	// 内容改为 B（同长度），再把 mtime 拨回原值 → 缓存命中但内容已变（C1 触发条件）
	if err := os.WriteFile(x, contentB, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(x, origMtime, origMtime); err != nil {
		t.Fatal(err)
	}

	// 二扫：若 C1 未修复，x 复用旧 full → 与 y 仍被判重复（误报）；
	// 修复后 x 实际抽样与缓存不符 → full 重算 → 与 y 不同 → 无重复组。
	p2 := New().WithCache(cch)
	g2, failed2, err := p2.Run(context.Background(), cfg)
	if err != nil || len(failed2) != 0 {
		t.Fatalf("二扫: failed=%+v err=%v", failed2, err)
	}
	if len(g2) != 0 {
		names := make([]string, 0, len(g2))
		for _, gg := range g2 {
			for _, f := range gg.Files {
				names = append(names, f.Path)
			}
		}
		t.Fatalf("C1 回归：内容已变却误判重复组（%d 组，文件 %v）；修复后应为 0 组（x/y 内容现已不同）",
			len(g2), names)
	}

	// 反向再验证：把 x 真正改回内容 A（含 mtime 复位），二扫应能找回重复组
	if err := os.WriteFile(x, contentA, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(x, origMtime, origMtime); err != nil {
		t.Fatal(err)
	}
	// 注意：y 此刻仍命中旧缓存（内容 A），x 内容恢复 A 但 mtime 与首扫一致 → 命中旧缓存 full
	p3 := New().WithCache(cch)
	g3, failed3, err := p3.Run(context.Background(), cfg)
	if err != nil || len(failed3) != 0 {
		t.Fatalf("三扫: failed=%+v err=%v", failed3, err)
	}
	if len(g3) != 1 {
		t.Fatalf("恢复内容后组数 = %d, want 1（x/y 内容应再次相同）", len(g3))
	}
}

// TestCacheSecondScanRefreshesLastHit R1 端到端：二扫全命中时，
// 命中条目 last_hit 必须被续期（否则热文件会在淘汰时最先被逐出）。
func TestCacheSecondScanRefreshesLastHit(t *testing.T) {
	root := t.TempDir()
	write := func(rel string, b []byte) string {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, b, 0o644)
		return p
	}
	content := []byte("refresh last_hit on cache hit")
	a := write("a.txt", content)
	write("b.txt", content)

	cch, err := cache.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cch.Close()

	cfg := model.ScanConfig{Roots: []string{root}, UseCache: true}
	if _, _, err := New().WithCache(cch).Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	h1, ok := cch.LastHit(a)
	if !ok {
		t.Fatal("首扫后应有缓存条目")
	}
	// last_hit 为秒级时间戳：跨秒确保可区分
	time.Sleep(1100 * time.Millisecond)
	if _, _, err := New().WithCache(cch).Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	h2, ok := cch.LastHit(a)
	if !ok {
		t.Fatal("二扫后条目不应消失")
	}
	if h2 <= h1 {
		t.Fatalf("命中未续期 last_hit: %d -> %d（R1 回归）", h1, h2)
	}
}
