package dedup

// AS-K1（2026-09-20 全仓审计）：缓存命中数必须是**可观测**的一等统计。
//
// 冒烟脚本 scripts/smoke-cli.sh 原先只把三跑结果互比，那证明的是"三跑结论
// 一致"，证明不了"缓存真的被用上"——If UseCache 断线（cfg 字段丢失、
// WithCache 传 nil、cacheOn 条件写错），三跑就等价于三次冷扫，照样全绿。
// 本项目恰恰有过 UseCache 口径的回归（B3-6 就是同类：口径看着对其实没生效）。
// 所以这里补一个正向信号：第二跑必须报出 >0 的命中。

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/cache"
	"filededup/internal/model"
)

func TestCacheHitsCounterReflectsLookupHits(t *testing.T) {
	root := t.TempDir()
	big := make([]byte, 200<<10) // >128KiB：走预筛+全量两阶段，才有缓存条目可言
	for i := range big {
		big[i] = byte(i * 7)
	}
	small := []byte("small duplicate content")
	for _, w := range []struct {
		rel string
		b   []byte
	}{
		{"g1/a.bin", big}, {"g1/b.bin", big},
		{"g2/s1.txt", small}, {"g2/s2.txt", small},
		{"u/uniq.bin", []byte("unique content here")},
	} {
		p := filepath.Join(root, w.rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, w.b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cch, err := cache.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cch.Close()
	cfg := model.ScanConfig{Roots: []string{root}, UseCache: true}

	// ① 首扫：库里什么都没有 → 命中必须为 0。
	//    负例的意义：若有人把计数器接在"读盘次数"之类的东西上，这里就不会是 0。
	p1 := New().WithCache(cch)
	if _, _, err := p1.Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if got := p1.CacheHits(); got != 0 {
		t.Fatalf("冷扫首跑命中数 = %d, want 0（库里还没有条目）", got)
	}

	// ② 二扫：4 个候选文件全部命中（uniq 因独 size 在阶段 1 就被淘汰，不入候选）。
	p2 := New().WithCache(cch)
	if _, _, err := p2.Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if got := p2.CacheHits(); got != 4 {
		t.Fatalf("复扫命中数 = %d, want 4（缓存已断线时这里是 0，正是本用例要拦的回归）", got)
	}

	// ③ 同一实例再跑：计数必须按轮归零，否则跨轮累加会让"本轮是否命中"失真。
	if _, _, err := p2.Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if got := p2.CacheHits(); got != 4 {
		t.Fatalf("同实例复跑命中数 = %d, want 4（不得跨轮累加）", got)
	}

	// ④ 未挂缓存的流水线：恒为 0，且不得 panic。
	p3 := New()
	if _, _, err := p3.Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if got := p3.CacheHits(); got != 0 {
		t.Fatalf("未启用缓存时命中数 = %d, want 0", got)
	}
}
