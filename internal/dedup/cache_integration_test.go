package dedup

// M4 集成：缓存命中二次扫描零读盘 + 结果一致性 + 修改后失效。

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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
