package dedup

// H1 端到端回归：旧两点（仅 head/tail）采样 + 缓存命中路径下，
// 「只改中段字节 + 拨回 mtime」的原地篡改对缓存完全不可见——
// 旧 full 哈希会让内容已变的文件继续与未改文件同组（误报 → 误删风险）。
// 修复后两层防线：① 缓存键含 dev/ino，路径被换成另一 inode ⇒ 未命中
// 〔2026-10-01 裁-2：ctime 已移出失效判据，"原地写必然推进 ctime → 未命中"这一腿不再成立〕；
// ② 即便身份层拦不住（同一 inode 上的原地改写、或身份未解析的卷），命中后重算的
// 4 点采样含中点，与缓存采样不符同样作废 full。本测试断言最终行为：不再出现误报组。
// ★ 本用例正是靠 ② 成立的：原地改 mid1 后 mtime 已拨回、dev/ino 未变 ⇒ ① 放行，
//   中段采样与缓存不符才把 x 打出 y 的桶。裁-2 之后这一格从"两层都拦"变成"只剩这一层"，
//   所以它比改前更有价值，不是可以顺手删掉的冗余。

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

	// failed 一并打印：组数少掉时，是"文件被读失败剔除"还是"身份被判为同一个"，
	// 只有失败清单能区分（windows CI 上曾只有 groups=0 这一条线索）。
	g1, fail1, err := New().WithCache(cch).Run(context.Background(), cfg)
	if err != nil || len(g1) != 1 {
		t.Fatalf("首扫应 1 组: groups=%d err=%v failed=%+v", len(g1), err, fail1)
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
