//go:build !windows

package ops

// Y4：清理操作有界并发的正确性验证（输出顺序确定、进度走满、并发安全）。

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"filededup/internal/model"
	"lukechampine.com/blake3"
)

// manyDuplicates 构造 n 个同内容副本的重复组（第 0 个为 keep）。
func manyDuplicates(t *testing.T, n int) (*model.DuplicateGroup, []string) {
	t.Helper()
	dir := t.TempDir()
	content := make([]byte, 2048)
	if _, err := rand.Read(content); err != nil {
		t.Fatal(err)
	}
	g := &model.DuplicateGroup{GroupID: 1, Hash: blake3.Sum256(content)}
	paths := make([]string, 0, n)
	for i := 0; i < n; i++ {
		p := filepath.Join(dir, fmt.Sprintf("dup%03d.bin", i))
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatal(err)
		}
		st, _ := os.Stat(p)
		g.Files = append(g.Files, &model.FileEntry{
			ID: uint64(i + 1), Path: p, Size: uint64(st.Size()),
			ModTime: st.ModTime().UnixNano(), Ext: ".bin",
		})
		paths = append(paths, p)
	}
	g.Reclaimable = uint64(len(paths)-1) * g.Files[0].Size
	return g, paths
}

// delete 并发执行：结果顺序必须与请求顺序一致，进度必须走满，文件确实删除。
func TestExecuteConcurrentDeleteOrdering(t *testing.T) {
	const n = 40
	g, paths := manyDuplicates(t, n)
	ids := make([]uint64, 0, n)
	for _, f := range g.Files {
		ids = append(ids, f.ID)
	}

	var mu sync.Mutex
	var lastDone, lastTotal int
	res := Execute(Options{
		Groups: []*model.DuplicateGroup{g},
		OnProgress: func(done, total int, _ string) {
			mu.Lock()
			lastDone, lastTotal = done, total
			mu.Unlock()
		},
	}, model.OpRequest{Kind: "delete", FileIDs: ids, ConfirmDanger: true})

	if len(res.Failed) != 0 {
		t.Fatalf("不应有失败项: %+v", res.Failed)
	}
	if len(res.OK) != n {
		t.Fatalf("OK 数 = %d, want %d", len(res.OK), n)
	}
	// 并发执行后输出顺序仍按请求顺序（下标收集 + 按序汇总）
	for i, want := range paths {
		if res.OK[i] != want {
			t.Fatalf("输出顺序错乱 @%d: %s != %s", i, res.OK[i], want)
		}
	}
	// 进度走满（含失败项亦计入完成）
	if lastDone != n || lastTotal != n {
		t.Fatalf("进度未走满: %d/%d", lastDone, lastTotal)
	}
	// 释放空间汇总正确（并发下无丢失更新）
	wantBytes := g.Files[0].Size * uint64(n)
	if res.Reclaimed != wantBytes {
		t.Fatalf("释放空间 = %d, want %d", res.Reclaimed, wantBytes)
	}
	for _, p := range paths {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("文件未删除: %s", p)
		}
	}
}

// hardlink 并发合并：全部副本必须指向同一 inode，内容不被破坏。
func TestExecuteConcurrentHardlink(t *testing.T) {
	const n = 24
	g, paths := manyDuplicates(t, n)
	keep := g.Files[0]
	dupIDs := make([]uint64, 0, n-1)
	for _, f := range g.Files[1:] {
		dupIDs = append(dupIDs, f.ID)
	}

	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{g},
		KeepIDs: map[uint64]bool{keep.ID: true},
	}, model.OpRequest{Kind: "hardlink", FileIDs: dupIDs})

	if len(res.Failed) != 0 || len(res.OK) != n-1 {
		t.Fatalf("并发合并失败: ok=%d failed=%+v", len(res.OK), res.Failed)
	}
	wantIno := inodeOf(t, keep.Path)
	ref, _ := os.ReadFile(keep.Path)
	for _, p := range paths[1:] {
		if got := inodeOf(t, p); got != wantIno {
			t.Fatalf("inode 不一致 %s: %d != %d", p, got, wantIno)
		}
		b, err := os.ReadFile(p)
		if err != nil || string(b) != string(ref) {
			t.Fatalf("内容不一致: %s", p)
		}
	}
}

// 混合结果（部分文件不存在）下，并发执行的分组统计与顺序仍正确。
func TestExecuteConcurrentMixedOutcomes(t *testing.T) {
	const n = 20
	g, paths := manyDuplicates(t, n)
	// 预先删除奇数下标文件 → 期望 Skipped
	for i := 1; i < n; i += 2 {
		if err := os.Remove(paths[i]); err != nil {
			t.Fatal(err)
		}
	}
	ids := make([]uint64, 0, n)
	for _, f := range g.Files {
		ids = append(ids, f.ID)
	}
	res := Execute(Options{Groups: []*model.DuplicateGroup{g}},
		model.OpRequest{Kind: "delete", FileIDs: ids, ConfirmDanger: true})

	if len(res.OK) != n/2 || len(res.Skipped) != n/2 || len(res.Failed) != 0 {
		t.Fatalf("分组统计错误: ok=%d skipped=%d failed=%d",
			len(res.OK), len(res.Skipped), len(res.Failed))
	}
	// Skipped 按请求顺序（S8 语义）
	for i, p := range res.Skipped {
		if p != paths[1+2*i] {
			t.Fatalf("Skipped 顺序错乱 @%d: %s", i, p)
		}
	}
	// Skipped 不计入释放空间
	if res.Reclaimed != g.Files[0].Size*uint64(n/2) {
		t.Fatalf("释放空间应只含 OK 项: %d", res.Reclaimed)
	}
}
