package ops

// P2 回归：清理操作必须可中止，且中止结果可解释。

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"filededup/internal/model"
)

// 取消后未派发的条目计入 Cancelled，且不得有任何条目被静默吞掉。
func TestExecuteHonoursContextCancel(t *testing.T) {
	fx := newFixture(t)
	dir := t.TempDir()
	content, _ := os.ReadFile(fx.orig.Path)
	extra := []*model.FileEntry{fx.orig, fx.dup1, fx.dup2}
	for i := 0; i < 40; i++ {
		p := filepath.Join(dir, "x.bin"+string(rune('A'+i)))
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatal(err)
		}
		st, _ := os.Stat(p)
		extra = append(extra, &model.FileEntry{
			ID: uint64(100 + i), Path: p, Size: uint64(st.Size()),
			ModTime: st.ModTime().UnixNano(), Ext: ".bin",
		})
	}
	fx.group.Files = extra

	ids := make([]uint64, 0, len(extra))
	for _, e := range extra {
		if e.ID != fx.orig.ID {
			ids = append(ids, e.ID)
		}
	}

	// 构造可被取消打断的慢速回收站：
	//   批量调用（len>1）先失败 → 触发执行器退化为逐文件派发（runIndexed），
	//   每项固定耗时 10ms，使 42 项总量远大于取消时延 → 部分完成可复现。
	// delete/move 同样逐条派发，但小文件删除过快，取消总在派发完毕后才到达。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	target := filepath.Join(t.TempDir(), "trash")
	resCh := make(chan struct{})
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
		close(resCh)
	}()

	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		KeepIDs: map[uint64]bool{fx.orig.ID: true},
		Ctx:     ctx,
		TrashFn: func(paths []string) (map[string]string, error) {
			if len(paths) > 1 {
				return nil, errFakeBatch // 强制走逐文件隔离路径
			}
			time.Sleep(10 * time.Millisecond)
			return mockTrash(target, false)(paths)
		},
	}, model.OpRequest{Kind: "trash", FileIDs: ids})
	<-resCh

	acc := len(res.OK) + len(res.Failed) + len(res.Skipped) + len(res.Cancelled)
	if acc != len(ids) {
		t.Fatalf("条目被吞: 请求 %d, 归类 %d (ok=%d fail=%d skip=%d cancel=%d)",
			len(ids), acc, len(res.OK), len(res.Failed), len(res.Skipped), len(res.Cancelled))
	}
	if len(res.OK) >= len(ids) {
		t.Fatalf("取消未生效：全部 %d 项仍执行完毕", len(res.OK))
	}
	if len(res.Cancelled) >= len(ids) {
		t.Fatalf("取消过晚，说明派发未被中止: %d", len(res.Cancelled))
	}
	if len(res.Cancelled) == 0 {
		t.Fatal("取消后未派发项应计入 Cancelled")
	}
}

var errFakeBatch = errors.New("批量入口不可用（测试注入）")

// 预先取消 → 一项都不该执行，文件必须原样留在磁盘。
// 这里用 trash 类型验证"批量入口前已取消则整批不派发"。
func TestExecuteAlreadyCancelledDoesNothing(t *testing.T) {
	fx := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	target := filepath.Join(t.TempDir(), "t")
	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		KeepIDs: map[uint64]bool{fx.orig.ID: true},
		Ctx:     ctx,
		TrashFn: mockTrash(target, false),
	}, model.OpRequest{Kind: "trash", FileIDs: []uint64{fx.dup1.ID, fx.dup2.ID}})
	if len(res.OK) != 0 {
		t.Fatalf("已取消时不得执行任何操作: %+v", res)
	}
	if len(res.Cancelled) != 2 {
		t.Fatalf("两项都应计入 Cancelled, got %d", len(res.Cancelled))
	}
	for _, p := range []string{fx.dup1.Path, fx.dup2.Path} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("取消后文件被移走: %s", p)
		}
	}
}

// 未取消时行为不变（Cancelled 恒为空，向后兼容）。
func TestExecuteWithoutCtxUnchanged(t *testing.T) {
	fx := newFixture(t)
	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		KeepIDs: map[uint64]bool{fx.orig.ID: true},
		TrashFn: mockTrash(filepath.Join(t.TempDir(), "t"), false),
	}, model.OpRequest{Kind: "trash", FileIDs: []uint64{fx.dup1.ID}})
	if len(res.OK) != 1 || len(res.Cancelled) != 0 {
		t.Fatalf("无 Ctx 时应正常执行: %+v", res)
	}
}
