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
//
// 2026-09-20 补充：本用例覆盖的是**不能验证回收站**的平台（darwin 等，
// TrashVerifiesRecycle 为默认 false）——其 Finder 落点解析可能配对失败而
// 返回空 dstMap，此时文件确实已进回收站，记 Failed 属误报。
// 「能验证却无落点」的严格分支见 TestTrashFallbackStrictFailsUnverifiedGone。
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

// TestTrashFallbackStrictFailsUnverifiedGone 2026-09-20 缺陷回归：
// **能验证回收站**的平台（Windows）在批量 trash 报错后回退时，
// 若某源路径已消失但平台从未报告其落点，说明无法证明文件进了回收站
// （典型的静默永久删除），必须记 **Failed** 并给出核实指引。
//
// 修正前这里记的是 Skipped —— 那会把数据丢失伪装成无害的「跳过」，
// 正是用户看到的现象：文件已移走、回收站却没有、界面还显示一切正常。
//
// 这是 defect 的**端到端**反例：断言的是最终状态码，而非某个内部函数。
func TestTrashFallbackStrictFailsUnverifiedGone(t *testing.T) {
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
			// 模拟"批量被静默永久删除"：把 dup1 从原位置**直接抹掉**
			// （不是移进 trashedDir，而是真正删除），然后整批返回错误。
			// 这与 SHFileOperation 静默永久删除后的现场完全一致：
			// 源不见了，回收站里也没有。
			if err := os.Remove(fx.dup1.Path); err != nil {
				t.Fatalf("模拟静默删除失败: %v", err)
			}
			return nil, errors.New("SHFileOperation 声称成功但复核未通过（模拟）")
		}
		// 回退逐文件：dup2 正常移入回收站
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
		Groups:               []*model.DuplicateGroup{fx.group},
		TrashFn:              trash,
		TrashVerifiesRecycle: true, // ← Windows：平台能验证回收站
	}, model.OpRequest{Kind: "trash", FileIDs: []uint64{fx.dup1.ID, fx.dup2.ID}})

	state := map[string]string{}
	for _, p := range res.OK {
		state[p] = "OK"
	}
	for _, p := range res.Skipped {
		state[p] = "Skipped"
	}
	for _, f := range res.Failed {
		state[f.Path] = "Failed"
	}
	if got := state[fx.dup1.Path]; got != "Failed" {
		t.Fatalf("★静默永久删除的文件必须记 Failed（提示用户核实），got %q\n"+
			"记成 Skipped 就是把数据丢失伪装成无害跳过——这正是缺陷本身", got)
	}
	// 失败原因必须可操作：告诉用户去哪里核实、下一步怎么做。
	var msg string
	for _, f := range res.Failed {
		if f.Path == fx.dup1.Path {
			msg = f.Err
		}
	}
	for _, kw := range []string{"回收站", "核实"} {
		if !strings.Contains(msg, kw) {
			t.Fatalf("失败原因 %q 缺少关键指引 %q", msg, kw)
		}
	}
	if got := state[fx.dup2.Path]; got != "OK" {
		t.Fatalf("回退重试成功的 dup2 应记 OK，got %q", got)
	}
}
