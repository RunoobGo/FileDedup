package dedup

// M315（CACHE-12）端到端：跨代混排的缓存行让**真重复对报不出来**。
//
// 次序（三轮扫描，缺一不可）：
//  1. P 与 Q 同为内容 A（大文件，等长）⇒ 成组，两行各存 fullA；
//  2. 原地把 P 等长改写成 B（head 段就不同 ⇒ 采样键换了）⇒ 这一轮 P 独占新采样桶、
//     没有同伴补算全量 ⇒ 写回 Full=nil。修前的 UPSERT CASE 会把 fullA 留在 P 行上，
//     与新的 size/mtime/partial 拼成"跨代混排行"；
//  3. 放入与 B 逐字节相同的 W ⇒ P（命中、沿用库存 fullA）与 W（未命中、现算 fullB）
//     同采样桶却落进不同 (size, full) 键 ⇒ **P/W 这对真重复不成组**。
//
// 修后：第 2 轮的写回把 P 的 full 清成 NULL ⇒ 第 3 轮两条都"未算全量"⇒ 阶段 3 各算一次
// ⇒ 同键成组。本用例钉的是这个可见差异（漏报），不是缓存内部形状
// （内部那格由 internal/cache/cache_m315_test.go 钉）。

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/cache"
	"filededup/internal/model"
)

// 大文件路径阈值：hasher.SmallFileMax = 128 KiB，小于它的文件一趟即得 full，
// 造不出"写回不带 full"的形状，故这里一律 256 KiB。
func m315Content(seed byte, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i%97) + seed
	}
	return b
}

func TestM315StaleFullDoesNotHideTrueDuplicates(t *testing.T) {
	root := t.TempDir()
	dir := func(rel string) string { return filepath.Join(root, rel) }
	size := 256 << 10

	contentA := m315Content(0x10, size)
	contentB := m315Content(0x20, size) // 等长、首段即不同

	pPath := dir("p.bin")
	qPath := dir("q.bin")
	if err := os.WriteFile(pPath, contentA, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(qPath, contentA, 0o644); err != nil {
		t.Fatal(err)
	}

	cch, err := cache.Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cch.Close()
	cfg := model.ScanConfig{Roots: []string{root}, UseCache: true}

	run := func(label string) []*model.DuplicateGroup {
		t.Helper()
		gs, failed, err := New().WithCache(cch).Run(context.Background(), cfg)
		if err != nil {
			t.Fatalf("%s: err=%v", label, err)
		}
		if len(failed) != 0 {
			t.Fatalf("%s: 不应有失败项：%+v", label, failed)
		}
		return gs
	}

	hasPair := func(gs []*model.DuplicateGroup, a, b string) bool {
		for _, g := range gs {
			var sa, sb bool
			for _, f := range g.Files {
				switch f.Path {
				case a:
					sa = true
				case b:
					sb = true
				}
			}
			if sa && sb {
				return true
			}
		}
		return false
	}

	// 第 1 轮：P/Q 成组，两行都拿到 fullA。
	if gs := run("首扫"); !hasPair(gs, pPath, qPath) {
		t.Fatalf("首扫 P/Q 未成组（前提不成立，后面的读数无意义）：%d 组", len(gs))
	}

	// 第 2 轮前：原地等长改写 P → B（mtime 自然推进；ctime 同）。
	if err := os.WriteFile(pPath, contentB, 0o644); err != nil {
		t.Fatal(err)
	}
	if gs := run("改写后首扫"); len(gs) != 0 {
		t.Fatalf("P 改成 B 后应无重复组（Q 仍是 A），实得 %d 组 ⇒ 前提不成立", len(gs))
	}

	// 第 3 轮前：放入与 B 逐字节相同的 W。
	wPath := dir("w.bin")
	if err := os.WriteFile(wPath, contentB, 0o644); err != nil {
		t.Fatal(err)
	}
	gs := run("放入同内容副本后")
	if !hasPair(gs, pPath, wPath) {
		t.Fatalf("M315：P 与 W 内容逐字节相同却没成组（缓存里 P 行仍带着上一代的 full）：%d 组", len(gs))
	}
	if hasPair(gs, qPath, wPath) {
		t.Fatalf("反向：Q（内容 A）不该与 W（内容 B）同组：%d 组", len(gs))
	}
}
