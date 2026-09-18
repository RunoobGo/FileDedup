package ops

// v0.5.0 功能 4：OnItem 逐项回调（写前日志的收口通道）与
// trash 的 src→dst 映射透传测试。

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"filededup/internal/model"
	"lukechampine.com/blake3"
)

func TestOnItemTrashDoneWithDest(t *testing.T) {
	fx := newFixture(t)
	target := filepath.Join(t.TempDir(), "t")
	var mu sync.Mutex
	var got []ItemResult
	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		KeepIDs: map[uint64]bool{fx.orig.ID: true},
		TrashFn: mockTrash(target, false),
		OnItem:  func(r ItemResult) { mu.Lock(); got = append(got, r); mu.Unlock() },
	}, model.OpRequest{Kind: "trash", FileIDs: []uint64{fx.orig.ID, fx.dup1.ID, fx.dup2.ID}})

	// 保留项被 S2 拒绝：计入 Failed 但不回调（它不在写前日志里）
	if len(res.Failed) != 1 || res.Failed[0].Path != fx.orig.Path {
		t.Fatalf("仅保留项应被拒绝: %+v", res.Failed)
	}
	if len(got) != 2 {
		t.Fatalf("回调 %d 次，期望 2（保留项不回调）: %+v", len(got), got)
	}
	byPath := map[string]ItemResult{}
	for _, r := range got {
		byPath[r.OrigPath] = r
	}
	for _, dup := range []string{fx.dup1.Path, fx.dup2.Path} {
		r, ok := byPath[dup]
		if !ok {
			t.Fatalf("%s 未回调", dup)
		}
		if r.State != "done" || r.DestPath != filepath.Join(target, filepath.Base(dup)) {
			t.Fatalf("%s 回调 = %+v", dup, r)
		}
	}
}

func TestOnItemVerifyStates(t *testing.T) {
	fx := newFixture(t)
	// dup1 篡改（verify Failed）；dup2 文件已消失（S8 Skipped）
	raw, _ := os.ReadFile(fx.dup1.Path)
	raw[len(raw)/2] ^= 0xFF
	os.WriteFile(fx.dup1.Path, raw, 0o644)
	os.Remove(fx.dup2.Path)

	var got []ItemResult
	Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		TrashFn: mockTrash(filepath.Join(t.TempDir(), "t"), false),
		OnItem:  func(r ItemResult) { got = append(got, r) },
	}, model.OpRequest{Kind: "trash", FileIDs: []uint64{fx.dup1.ID, fx.dup2.ID, 999}})

	if len(got) != 2 {
		t.Fatalf("回调 %d 次，期望 2（结果集外 id 不回调）: %+v", len(got), got)
	}
	st := map[string]string{}
	for _, r := range got {
		st[r.OrigPath] = r.State
	}
	if st[fx.dup1.Path] != "failed" || st[fx.dup2.Path] != "skipped" {
		t.Fatalf("状态 = %+v", st)
	}
	for _, r := range got {
		if r.State == "failed" && r.Err == "" {
			t.Fatalf("failed 必须带原因: %+v", r)
		}
	}
}

func TestOnItemMoveAndHardlink(t *testing.T) {
	t.Run("move", func(t *testing.T) {
		fx := newFixture(t)
		target := t.TempDir()
		var got []ItemResult
		res := Execute(Options{
			Groups: []*model.DuplicateGroup{fx.group},
			OnItem: func(r ItemResult) { got = append(got, r) },
		}, model.OpRequest{Kind: "move", FileIDs: []uint64{fx.dup1.ID}, TargetDir: target})
		if len(res.OK) != 1 {
			t.Fatalf("move 应成功: %+v", res)
		}
		if len(got) != 1 || got[0].State != "done" ||
			got[0].DestPath != filepath.Join(target, "dup1.bin") {
			t.Fatalf("move 回调 = %+v", got)
		}
	})
	t.Run("hardlink", func(t *testing.T) {
		fx := newFixture(t)
		var got []ItemResult
		res := Execute(Options{
			Groups:  []*model.DuplicateGroup{fx.group},
			KeepIDs: map[uint64]bool{fx.orig.ID: true},
			OnItem:  func(r ItemResult) { got = append(got, r) },
		}, model.OpRequest{Kind: "hardlink", FileIDs: []uint64{fx.dup1.ID}})
		if len(res.OK) != 1 {
			t.Fatalf("hardlink 应成功: %+v", res)
		}
		if len(got) != 1 || got[0].State != "done" || got[0].LinkSrc != fx.orig.Path ||
			got[0].DestPath != "" {
			t.Fatalf("hardlink 回调 = %+v", got)
		}
	})
}

// 取消：未派发项不回调（由 App Finalize 归 cancelled），回调次数恒等于
// 已归类（OK/Failed/Skipped）条目数。
func TestOnItemCancelUndispatchedSilent(t *testing.T) {
	dir := t.TempDir()
	g, files := mkSameContentGroup(t, dir, 24)
	target := filepath.Join(dir, "t")
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var got []ItemResult
	errBatch := errors.New("批量失败，强制逐文件")
	resCh := make(chan model.OpsResult, 1)
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	go func() {
		resCh <- Execute(Options{
			Groups: []*model.DuplicateGroup{g},
			TrashFn: func(paths []string) (map[string]string, error) {
				if len(paths) > 1 {
					return nil, errBatch
				}
				time.Sleep(5 * time.Millisecond)
				return mockTrash(target, false)(paths)
			},
			Ctx:    ctx,
			OnItem: func(r ItemResult) { mu.Lock(); got = append(got, r); mu.Unlock() },
		}, model.OpRequest{Kind: "trash", FileIDs: fileIDs(files)})
	}()
	res := <-resCh
	mu.Lock()
	defer mu.Unlock()
	processed := len(res.OK) + len(res.Failed) + len(res.Skipped)
	if len(got) != processed {
		t.Fatalf("回调 %d 次 != 已归类 %d（取消项被多回调？）: cancelled=%d",
			len(got), processed, len(res.Cancelled))
	}
	if len(res.Cancelled) == 0 {
		t.Fatal("未触发取消，测试无效")
	}
}

// mkSameContentGroup 同内容 n 员单组（取消测试用批量派发面）。
func mkSameContentGroup(t *testing.T, dir string, n int) (*model.DuplicateGroup, []*model.FileEntry) {
	t.Helper()
	content := make([]byte, 1024)
	rand.Read(content)
	g := &model.DuplicateGroup{GroupID: 1, Hash: blake3.Sum256(content)}
	for i := 0; i < n; i++ {
		p := filepath.Join(dir, fmt.Sprintf("f%02d.bin", i))
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatal(err)
		}
		st, _ := os.Stat(p)
		e := &model.FileEntry{
			ID: uint64(i + 1), Path: p, Size: uint64(st.Size()),
			ModTime: st.ModTime().UnixNano(), Ext: ".bin",
		}
		g.Files = append(g.Files, e)
	}
	return g, g.Files
}

func fileIDs(files []*model.FileEntry) []uint64 {
	ids := make([]uint64, len(files))
	for i, f := range files {
		ids[i] = f.ID
	}
	return ids
}
