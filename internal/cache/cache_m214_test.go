package cache

// M214（2026-09-24 第五轮审查批）：停用位必须盖住全部四个发 SQL 的对外触点。
// 改前 Lookup/Store/Touch 有 corrupt 闸而 Clear/GetStats/LastHit 没有 ⇒
// 库被确证损坏停用后，界面每轮拉统计/清缓存/诊断续期仍向死库发 SQL，
// 每轮都拿回一条原始英文错误。本文件即"修前真红"探针（对改前 cache.go
// 三条断言全部红，读数见 §6.40）。

import (
	"errors"
	"path/filepath"
	"testing"

	"filededup/internal/fsid"
)

func TestM214DisabledCacheStopsAllSQLLegs(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(filepath.Join(dir, "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if err := c.Store([]Entry{{Path: "/x/a", Size: 7, MtimeNs: 7, Head: 3, Tail: 4}}); err != nil {
		t.Fatal(err)
	}
	// 前提自检：停用位落下之前，三个触点都真实触库（否则下面的断言是空转）。
	if _, ok, _ := c.Lookup("/x/a", 7, 7, fsid.ID{}); !ok {
		t.Fatal("前提：Store 之后 Lookup 应命中")
	}
	if ts, ok := c.LastHit("/x/a"); !ok || ts == 0 {
		t.Fatalf("前提：LastHit 应读到续期值，got (%d,%v)", ts, ok)
	}
	if st, err := c.GetStats(); err != nil || st.Entries != 1 {
		t.Fatalf("前提：GetStats 应报 1 条，got %+v err=%v", st, err)
	}

	// 模拟 M74 的运行期确证损坏（内部测试直接置停用位）。
	c.corrupt.Store(true)

	// 停用位落下之后的三格。**顺序即判据的一部分**：改前 Clear 会真的发 DELETE
	// 把行清掉，若把 Clear 排最前，LastHit 那格改前也读绿（行没了自然 (0,false)）
	// ——本批首跑就栽在这里，取红必须验"红的是不是那一格"（§6.15 口径 1 的同族）。
	if ts, ok := c.LastHit("/x/a"); ts != 0 || ok {
		t.Errorf("停用后 LastHit 必须 (0,false)，got (%d,%v)", ts, ok)
	}
	st, err := c.GetStats()
	if err != nil || st != (Stats{}) {
		t.Errorf("停用后 GetStats 必须零值+nil（不发 SQL），got %+v err=%v", st, err)
	}
	if err := c.Clear(); !errors.Is(err, ErrCorruptDisabled) {
		t.Errorf("停用后 Clear 必须返回 ErrCorruptDisabled（不再发 DELETE），got %v", err)
	}
	// Corrupted() 仍是唯一真状态出口（话术边界：停用 ≠ 已隔离/已重建）。
	if !c.Corrupted() {
		t.Error("停用位语义漂移：Corrupted 应为 true")
	}
}
