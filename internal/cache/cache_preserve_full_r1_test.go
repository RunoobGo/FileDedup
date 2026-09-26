package cache

// R-缓存-1（2026-09-24 审查五轮 F 批，J-4=(b) 随批修，性能向/方向安全）：
// Store 的 UPSERT 无条件写 `full=excluded.full`。当第二轮对同一路径只算到 partial
// （大文件预筛后被淘汰，Entry.Full=nil → SQL NULL）时，它会把上一轮已算好的**有效**
// full 抹成 NULL —— 下一次扫描本该直接命中 full 免算，却被逼着在阶段 3 重算全量。
// partial 早就用 CASE 防了同款覆盖（cache.go:350），full 却没有。修法比照 partial：
// excluded.full 为 NULL 时保留原值。方向安全：从不写入错误的 full，只是不丢已有的。

import (
	"path/filepath"
	"testing"

	"filededup/internal/fsid"
)

func TestStorePreservesExistingFullWhenNewEntryHasNone(t *testing.T) {
	c, err := Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	full := make([]byte, 32)
	for i := range full {
		full[i] = byte(i + 1)
	}
	base := Entry{Path: "/x/a.bin", Size: 100, MtimeNs: 1, Head: 11, Tail: 22, Mid1: 33, Mid2: 44}

	// 第一轮：带有效 full。
	e1 := base
	e1.Full = full
	if err := c.Store([]Entry{e1}); err != nil {
		t.Fatal(err)
	}

	// 第二轮：同路径同 size/mtime，但这次没算 full（Full=nil）。
	e2 := base
	e2.Full = nil
	if err := c.Store([]Entry{e2}); err != nil {
		t.Fatal(err)
	}

	got, hit, fullValid := c.Lookup("/x/a.bin", 100, 1, fsid.ID{})
	if !hit {
		t.Fatal("应命中缓存")
	}
	if !fullValid {
		t.Fatal("R-缓存-1：第二轮 Store 未带 full，却把既有有效 full 抹成 NULL（fullValid=false）")
	}
	if len(got.Full) != 32 {
		t.Fatalf("full 应保留 32 字节，实得 %d", len(got.Full))
	}
	for i := range full {
		if got.Full[i] != full[i] {
			t.Fatalf("full 内容第 %d 字节被改写：got %d want %d", i, got.Full[i], full[i])
		}
	}
}

// 负控制方向：新一轮**带**不同的 full 时必须覆盖旧值（CASE 只挡 NULL，不挡真值）。
func TestStoreOverwritesFullWhenNewEntryHasOne(t *testing.T) {
	c, err := Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	base := Entry{Path: "/y/b.bin", Size: 200, MtimeNs: 2, Head: 1, Tail: 2, Mid1: 3, Mid2: 4}
	old := make([]byte, 32)
	e1 := base
	e1.Full = old
	if err := c.Store([]Entry{e1}); err != nil {
		t.Fatal(err)
	}

	nw := make([]byte, 32)
	for i := range nw {
		nw[i] = 0xAB
	}
	e2 := base
	e2.Full = nw
	if err := c.Store([]Entry{e2}); err != nil {
		t.Fatal(err)
	}

	got, hit, fullValid := c.Lookup("/y/b.bin", 200, 2, fsid.ID{})
	if !hit || !fullValid {
		t.Fatalf("应命中且 full 有效：hit=%v fullValid=%v", hit, fullValid)
	}
	for i := range nw {
		if got.Full[i] != nw[i] {
			t.Fatalf("新 full 未覆盖旧值（第 %d 字节 got %d want %d）", i, got.Full[i], nw[i])
		}
	}
}
