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

// TestOnItemBatchTrashPartialDestKept 回归（2026-09-18 全量审查 C2）：
// 批量 trash 失败时，平台实现会连同 error 一起带回「已成功部分」的
// src→dst 映射（darwin 的 osascript 批次、linux 的 trashXDG 逐个执行皆如此）。
// 修复前 err 分支整包丢弃该映射，回退只能把已入站文件记成去向为空的
// Skipped，而 Skipped 永不进回撤（app.go UndoOperation 只认 done）
// → 这批已在回收站的文件永久失去应用内回撤。
func TestOnItemBatchTrashPartialDestKept(t *testing.T) {
	dir := t.TempDir()
	g, files := mkSameContentGroup(t, dir, 4)
	target := t.TempDir()
	var mu sync.Mutex
	var got []ItemResult
	batchMoved := map[string]string{}
	res := Execute(Options{
		Groups: []*model.DuplicateGroup{g},
		TrashFn: func(paths []string) (map[string]string, error) {
			if len(paths) > 1 {
				// 模拟半途失败：前两个已移入回收站，整批仍返回 error
				for _, p := range paths[:2] {
					d := filepath.Join(target, filepath.Base(p))
					if err := os.Rename(p, d); err != nil {
						t.Fatal(err)
					}
					batchMoved[p] = d
				}
				return batchMoved, errors.New("批量在第 3 个文件失败（模拟）")
			}
			return mockTrash(target, false)(paths)
		},
		OnItem: func(r ItemResult) { mu.Lock(); got = append(got, r); mu.Unlock() },
	}, model.OpRequest{Kind: "trash", FileIDs: fileIDs(files)})

	if len(got) != len(files) {
		t.Fatalf("收口 %d 次，期望 %d: %+v", len(got), len(files), got)
	}
	byPath := map[string]ItemResult{}
	for _, r := range got {
		byPath[r.OrigPath] = r
	}
	for p, d := range batchMoved {
		r, ok := byPath[p]
		if !ok {
			t.Fatalf("批量已入站文件未收口: %s", p)
		}
		if r.State != "done" || r.DestPath != d {
			t.Fatalf("批量已入站文件须收口为 done+已知去向，实为 %+v", r)
		}
	}
	if len(res.Skipped) != 0 {
		t.Fatalf("已入站文件不得记为 Skipped（Skipped 不进回撤）: %+v", res.Skipped)
	}
	if len(res.OK) != len(files) {
		t.Fatalf("OK %d，期望 %d（2 个批量 + 2 个回退）: %+v", len(res.OK), len(files), res)
	}
}

// TestOnItemClosedBeforeLaterFilesMove 回归（2026-09-18 全量审查 C6）：
// 账本收口必须紧跟每次 syscall，而不是等全批走完在 aggregate 里统一补记
// ——批内被杀/退出时，延后收口会让全部条目停在 planned，重启后归为
// interrupted，而回撤只认 done。
// 判据取 move 分支（串行派发，时序确定）：首条收口发生时，后续文件必须
// 仍在原位。修复前所有条目在 Execute 末尾一次性回调，此刻文件已全部移走。
func TestOnItemClosedBeforeLaterFilesMove(t *testing.T) {
	dir := t.TempDir()
	g, files := mkSameContentGroup(t, dir, 6)
	target := t.TempDir()
	remaining := -1
	Execute(Options{
		Groups: []*model.DuplicateGroup{g},
		OnItem: func(r ItemResult) {
			if remaining >= 0 {
				return // 只关心首条收口的时刻
			}
			n := 0
			for _, f := range files {
				if f.Path != r.OrigPath {
					if _, err := os.Stat(f.Path); err == nil {
						n++
					}
				}
			}
			remaining = n
		},
	}, model.OpRequest{Kind: "move", FileIDs: fileIDs(files), TargetDir: target})

	if want := len(files) - 1; remaining != want {
		t.Fatalf("首条收口时后续文件剩余 %d，期望 %d：收口未紧跟 syscall，"+
			"批内被杀将导致全部条目停在 planned 而不可回撤", remaining, want)
	}
}
