package dedup

// H1 端到端回归：旧两点（仅 head/tail）采样 + 缓存命中路径下，
// 「只改中段字节 + 拨回 mtime」的原地篡改对缓存完全不可见——
// 旧 full 哈希会让内容已变的文件继续与未改文件同组（误报 → 误删风险）。
// 修复后两层防线：① 缓存键含 dev/ino/ctime_ns，原地写必然推进 ctime → 未命中；
// ② 即便身份层失效（如无 ctime 的怪异卷），命中后重算的 4 点采样含中点，
// 与缓存采样不符同样作废 full。本测试断言最终行为：不再出现误报组。

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"filededup/internal/cache"
	"filededup/internal/model"
)

func TestCacheMidOnlyChangeNoFalseGroup(t *testing.T) {
	root := t.TempDir()
	// 200KiB > SmallFileMax：走两阶段（4 点预筛 + 全量）。
	// 采样窗口：head [0,64K)、mid1 [100K,164K)、mid2/tail [136K,200K)。
	n := 200 << 10
	content := make([]byte, n)
	for i := range content {
		content[i] = byte(i * 7)
	}
	x := filepath.Join(root, "x.bin")
	y := filepath.Join(root, "y.bin")
	if err := os.WriteFile(x, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(y, content, 0o644); err != nil {
		t.Fatal(err)
	}

	cch, err := cache.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cch.Close()
	cfg := model.ScanConfig{Roots: []string{root}, UseCache: true}

	g1, _, err := New().WithCache(cch).Run(context.Background(), cfg)
	if err != nil || len(g1) != 1 {
		t.Fatalf("首扫应 1 组: groups=%d err=%v", len(g1), err)
	}
	origMtime := func() time.Time {
		st, err := os.Stat(x)
		if err != nil {
			t.Fatal(err)
		}
		return st.ModTime()
	}()

	// 原地只改 mid1 窗口内 1 个字节（head/tail 窗口不动），随后拨回 mtime。
	f, err := os.OpenFile(x, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte{0xEE}, int64(n/2)+8); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(x, origMtime, origMtime); err != nil {
		t.Fatal(err)
	}

	g2, _, err := New().WithCache(cch).Run(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(g2) != 0 {
		t.Fatalf("H1 回归：中段被改且 mtime 已拨回，仍误判 %d 组（应为 0）", len(g2))
	}
}
