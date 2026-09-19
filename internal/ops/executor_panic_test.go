package ops

// C7/C6 回归：runIndexed 的 worker 内 panic 守卫、trash 回退状态一致性。

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"filededup/internal/model"
)

// TestRunIndexedPanicGuard 白盒：worker 内 panic 必须被捕获，
// 不崩溃进程、回调 onPanic 且正确携带下标 i 与 panic 值。
func TestRunIndexedPanicGuard(t *testing.T) {
	const n = 8
	const panicAt = 3
	var mu sync.Mutex
	var got []int
	runIndexed(context.Background(), n, 4, func(i int) {
		if i == panicAt {
			panic("boom")
		}
	}, func(i int, r any) {
		mu.Lock()
		got = append(got, i)
		mu.Unlock()
		if i != panicAt {
			t.Errorf("onPanic 下标错误: got %d want %d (r=%v)", i, panicAt, r)
		}
		s, ok := r.(string)
		if !ok || s != "boom" {
			t.Errorf("onPanic 应收到 panic 值 \"boom\"，got %v", r)
		}
	})
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0] != panicAt {
		t.Fatalf("onPanic 调用次数/下标 = %v, want [%d]", got, panicAt)
	}
}

// TestExecuteWorkerPanicContained 集成：trash 批量失败退化为逐文件执行时，
// 逐文件 panic 不得崩溃进程，且对应文件计入 Failed（而非遗漏或崩进程）。
func TestExecuteWorkerPanicContained(t *testing.T) {
	fx := newFixture(t)
	var calls int32
	var mu sync.Mutex
	// 批量调用（len>1）先失败 → 触发执行器退化为逐文件派发（runIndexed），
	// 逐文件调用 panic → 命中 C7 守卫。两个 dup 都应被标记为失败且进程存活。
	trash := func(paths []string) (map[string]string, error) {
		mu.Lock()
		c := calls
		calls++
		mu.Unlock()
		if c == 0 {
			return nil, errors.New("批量 trash 失败（触发退化）")
		}
		panic("worker boom")
	}
	var onPanicCalls int32
	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		TrashFn: trash,
		OnPanic: func(err error) { atomic.AddInt32(&onPanicCalls, 1) },
	}, model.OpRequest{Kind: "trash", FileIDs: []uint64{fx.dup1.ID, fx.dup2.ID}})

	if len(res.Failed) != 2 {
		t.Fatalf("两个 dup 都应计入 Failed，got %d (OK=%v Skipped=%v)", len(res.Failed), res.OK, res.Skipped)
	}
	for _, f := range res.Failed {
		if f.Stage != "ops" || !strings.Contains(f.Err, "panic 已被捕获") {
			t.Errorf("失败项应标注 panic 捕获，got %+v", f)
		}
	}
	if atomic.LoadInt32(&onPanicCalls) != 2 {
		t.Fatalf("OnPanic 应被回调 2 次，got %d", onPanicCalls)
	}
	// 进程存活：到这里没崩即通过（崩溃会让测试直接失败）。
}

// TestTrashFallbackSkipsAlreadyTrashed C6 回归：批量 trash 部分成功后报错，
// 回退重跑不得把已移入回收站的文件记为 Failed（结果集会错误地显示「仍存在」）。
// 源已不在的条目按 S8 语义记 Skipped（目标已达成），仅重试仍在源的条目。
func TestTrashFallbackSkipsAlreadyTrashed(t *testing.T) {
	fx := newFixture(t)
	var calls int32
	var mu sync.Mutex
	trashedDir := t.TempDir()
	trash := func(paths []string) (map[string]string, error) {
		mu.Lock()
		c := calls
		calls++
		mu.Unlock()
		if c == 0 {
			// 模拟批量部分成功：移走 dup1 后整批返回错误（无从告知哪些已成功）
			if err := os.Rename(fx.dup1.Path,
				filepath.Join(trashedDir, "dup1.bin")); err != nil {
				t.Fatalf("模拟部分成功失败: %v", err)
			}
			return nil, errors.New("批量 trash 失败（模拟）")
		}
		// 回退逐文件：成功移入
		dst := map[string]string{}
		for _, p := range paths {
			d := filepath.Join(trashedDir, filepath.Base(p))
			if err := os.Rename(p, d); err != nil {
				return dst, err
			}
			dst[p] = d
		}
		return dst, nil
	}
	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		TrashFn: trash,
	}, model.OpRequest{Kind: "trash", FileIDs: []uint64{fx.dup1.ID, fx.dup2.ID}})

	found := map[string]string{}
	for _, p := range res.OK {
		found[p] = "OK"
	}
	for _, p := range res.Skipped {
		found[p] = "Skipped"
	}
	for _, f := range res.Failed {
		found[f.Path] = "Failed"
	}
	if got := found[fx.dup1.Path]; got != "Skipped" {
		t.Errorf("已被批量移走的 dup1 应记 Skipped（S8 目标已达成），got %s", got)
	}
	if got := found[fx.dup2.Path]; got != "OK" {
		t.Errorf("回退重试成功的 dup2 应记 OK，got %s", got)
	}
}
