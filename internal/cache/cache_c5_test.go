package cache

// C5/G11 回归：GetStats 改读锁 + 触发式计数。
//   - 修正前 GetStats 取写锁 + 每次全表 COUNT：阻塞并发 Lookup，统计成本恒定高。
//   - 修正后：读锁；计数在写锁内维护（写后由 evictLocked 即时重算），
//     Clear 后首查走一次 COUNT（不回写，避免持读锁写共享字段）。

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func mustOpenC5(t *testing.T) *Cache {
	t.Helper()
	c, err := Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// TestGetStatsCountsAfterWriteAndClear 数值正确性：写入后统计准确，Clear 后归零。
func TestGetStatsCountsAfterWriteAndClear(t *testing.T) {
	c := mustOpenC5(t)

	st, err := c.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Entries != 0 || st.WithFull != 0 {
		t.Fatalf("空库统计 = %+v, want 0/0", st)
	}

	ents := []Entry{
		{Path: "/a", Size: 1, MtimeNs: 1, Head: 0x11, Tail: 0x22, Full: make([]byte, 32)},
		{Path: "/b", Size: 2, MtimeNs: 2, Head: 0x33, Tail: 0x44}, // 无 full
	}
	if err := c.Store(ents); err != nil {
		t.Fatal(err)
	}
	st, err = c.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Entries != 2 || st.WithFull != 1 {
		t.Fatalf("写入后统计 = %+v, want entries=2 withFull=1", st)
	}

	if err := c.Clear(); err != nil {
		t.Fatal(err)
	}
	st, err = c.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Entries != 0 || st.WithFull != 0 {
		t.Fatalf("Clear 后统计 = %+v, want 0/0（C5：Clear 须使缓存计数失效）", st)
	}
	if st.LastEvicted != 0 {
		t.Fatalf("Clear 后 lastEvicted = %d, want 0", st.LastEvicted)
	}
}

// TestGetStatsConcurrentWithWriters 并发回归：GetStats（读锁）与 Store/Lookup
// 并发不产生数据竞争（-race 下跑）；统计值始终等于已提交写入的单调前缀。
func TestGetStatsConcurrentWithWriters(t *testing.T) {
	c := mustOpenC5(t)

	const rounds = 200
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { // 写者
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			e := Entry{Path: fmt.Sprintf("/p/%d", i), Size: uint64(i), MtimeNs: int64(i),
				Head: uint64(i), Tail: uint64(i)}
			if i%2 == 0 {
				e.Full = make([]byte, 32)
			}
			if err := c.Store([]Entry{e}); err != nil {
				t.Errorf("Store: %v", err)
				return
			}
		}
	}()
	go func() { // 读命中路径
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			c.Lookup(fmt.Sprintf("/p/%d", i), uint64(i), int64(i))
		}
	}()
	go func() { // 统计读者
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			st, err := c.GetStats()
			if err != nil {
				t.Errorf("GetStats: %v", err)
				return
			}
			// 计数只能取到已提交写入的单调前缀（读锁与写锁互斥保证）
			if st.Entries < 0 || st.Entries > rounds || st.WithFull > st.Entries {
				t.Errorf("统计越界: %+v", st)
				return
			}
		}
	}()
	wg.Wait()

	st, err := c.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Entries != rounds {
		t.Fatalf("并发写后 entries = %d, want %d", st.Entries, rounds)
	}
	if st.WithFull != rounds/2+rounds%2 {
		t.Fatalf("并发写后 withFull = %d, want %d", st.WithFull, rounds/2+rounds%2)
	}
}
