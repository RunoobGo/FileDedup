package cache

// M285（2026-09-27 实施批；04 §6.51 立账，源头是 W9-9 的半格真机读数）：
// 缓存库「确证损坏 → 改名隔离 → 重建」这条出口原先只有 stderr。GUI 用户没有终端，
// 而这里丢的东西有实际后果——**整表作废**：本次运行的每次扫描都要全量重算，
// 界面却不发一声。history 侧的同形问题在 M12b 已经用 QuarantinedTo 收口，cache 侧一直漏着。
//
// 判据与 internal/history/history_m12_test.go 同源，两条缺一不可：
//   - 健康库必须交空串，否则每次启动白报一次「缓存丢了」（假警会被用户脱敏，真警跟着失效）；
//   - 隔离后必须给出**仍可读**的隔离文件名——界面文案里「旧文件仍在配置目录」那句要能兑现。
// 另外钉住重建后的库真能写：文案里说了「重建」，写不进去就是另一句假话。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuarantinedToVisibleToCaller(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cache.db")

	c, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.QuarantinedTo(); got != "" {
		t.Fatalf("健康库的 QuarantinedTo = %q，应为空串", got)
	}
	if err := c.Store([]Entry{{Path: "/keep/me", Size: 7, MtimeNs: 7, Head: 3, Tail: 4}}); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	// 覆写成非库影像 ⇒ 触发「确证损坏 → 隔离重建」那一条路（与 app_m25_test.go 的
	// 失败夹具刻意不同：建成目录走的是 Open 直接报错，永不隔离）。
	junk := []byte("garbage where a hash cache used to live")
	if err := os.WriteFile(p, junk, 0o644); err != nil {
		t.Fatal(err)
	}
	c2, err := Open(p)
	if err != nil {
		t.Fatalf("确证损坏应隔离重建: %v", err)
	}
	defer c2.Close()

	q := c2.QuarantinedTo()
	if q == "" {
		t.Fatal("隔离重建后 QuarantinedTo 为空：应用层无从得知，界面只会显示缓存是空的")
	}
	if _, err := os.Stat(q); err != nil {
		t.Fatalf("隔离文件应仍可读（文案据此承诺「旧文件仍在配置目录」）: %v", err)
	}
	payload, err := os.ReadFile(q)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != string(junk) {
		t.Fatalf("隔离内容被改写: %q", payload)
	}
	if !strings.HasPrefix(filepath.Base(q), "cache.db.broken-") {
		t.Fatalf("隔离文件名 = %q，期望 cache.db.broken-* 形态", q)
	}

	// 重建后的库必须真的可写（否则"已重建"是假的）
	if err := c2.Store([]Entry{{Path: "/after/rebuild", Size: 9, MtimeNs: 9, Head: 1, Tail: 2}}); err != nil {
		t.Fatalf("重建后的库不可写: %v", err)
	}
	st, err := c2.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	// 整表作废的真身：隔离前那 1 条（/keep/me）必须不在了，只剩新写的这条。
	if st.Entries != 1 {
		t.Fatalf("重建后条目数 = %d，期望 1（旧表应随隔离整表作废）：%+v", st.Entries, st)
	}
}
