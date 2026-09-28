package cache

// 补审面（2026-09-28 第七轮，子代理 B-4/B-5）的两条判据。
//
// 两条都是"统计页在说一件不是事实的事"，方向相反但同源：`evictLocked` 只维护了
// `cnt` 一个计数器，其余（`cntFull` / `lastEvicted`）要么停在旧值、要么只在 Clear
// 时归零。缓存页那三格数字因此可能是**不同时刻**的读数。
//
// ★ 这两条不是"装饰用例"：变异做法写在每条下面，都是**改前红**的。

import (
	"testing"
)

// storeSome 写入 n 条，其中前 fullN 条带全量哈希（Full 32 字节）。
func storeSome(t *testing.T, c *Cache, n, fullN int) {
	t.Helper()
	ents := make([]Entry, 0, n)
	for i := 0; i < n; i++ {
		e := Entry{Path: "/p" + string(rune('a'+i)), Size: uint64(i + 1), MtimeNs: int64(i + 1),
			Head: uint64(100 + i), Tail: uint64(200 + i), Mid1: 1, Mid2: 2}
		if i < fullN {
			full := make([]byte, 32)
			full[0] = byte(i + 1)
			e.Full = full
		}
		ents = append(ents, e)
	}
	if err := c.Store(ents); err != nil {
		t.Fatalf("Store: %v", err)
	}
}

// 判据一（B-4）：淘汰之后 `GetStats().Entries` 必须是**库里真实的条数**。
//
// 变异（改前形状）：把 `c.cntValid = false` 那一行删掉 ⇒ 修前 cntValid 仍为真，
// GetStats 直接交出 `c.cnt`（= MaxEntries，一个被伪造出来的 500000），而库里只剩
// 3 条 ⇒ 本条当场红。
func TestEvictedStatsEntriesMatchReality(t *testing.T) {
	c := openTest(t)
	const total, withFull = 5, 3
	storeSome(t, c, total, withFull)

	// 伪造"超限 2 条"：既有手法（cache_m73_m75_test.go 同形），真 DELETE 会删 2 条。
	c.mu.Lock()
	c.cnt = MaxEntries + 2
	c.cntValid = true
	err := c.evictLocked()
	c.mu.Unlock()
	if err != nil {
		t.Fatalf("evictLocked: %v", err)
	}

	st, err := c.GetStats()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	const want = 3 // 5 条删掉 2 条
	if st.Entries != want {
		t.Fatalf("淘汰后 Entries = %d, want %d：统计页报的是淘汰前的计数口径，"+
			"与库里的真实条数对不上（用户据此判断「缓存满没满」会全错）", st.Entries, want)
	}
	if st.WithFull > st.Entries {
		t.Fatalf("WithFull(%d) > Entries(%d)：两个数取自不同时刻 ⇒ 统计页自相矛盾",
			st.WithFull, st.Entries)
	}
}

// 判据二（B-5）：本轮没有发生淘汰时，`LastEvicted` 必须是 0。
//
// 变异（改前形状）：把 `c.cnt <= MaxEntries` 分支里的 `c.lastEvicted = 0` 删掉 ⇒
// 第一次淘汰写入的数字会在后续每一轮 CacheStats 里反复出现 ⇒ 本条当场红。
func TestLastEvictedZeroWhenNoEvictionHappened(t *testing.T) {
	c := openTest(t)
	storeSome(t, c, 2, 1)

	// 先制造一次真实淘汰（同上：伪造超限 1 条）
	c.mu.Lock()
	c.cnt = MaxEntries + 1
	c.cntValid = true
	if err := c.evictLocked(); err != nil {
		c.mu.Unlock()
		t.Fatalf("evictLocked: %v", err)
	}
	c.mu.Unlock()
	first, err := c.GetStats()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if first.LastEvicted != 1 {
		t.Fatalf("刚淘汰 1 条，LastEvicted = %d, want 1（前提自检：这轮确实淘汰过）", first.LastEvicted)
	}

	// 再来一轮：库里已不超限 ⇒ 不该再报"最近淘汰 1"
	c.mu.Lock()
	err = c.evictLocked()
	c.mu.Unlock()
	if err != nil {
		t.Fatalf("第二轮 evictLocked: %v", err)
	}
	second, err := c.GetStats()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if second.LastEvicted != 0 {
		t.Fatalf("本轮未淘汰却报 LastEvicted = %d：界面会反复显示同一个「最近淘汰 N」，"+
			"读起来像每轮都在淘汰", second.LastEvicted)
	}
}
