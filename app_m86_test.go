package main

// M86（04 §6.11 OPS-14b）的 **app 腿**探针（设计段 §19.3 P-19-7b）。
//
// ops.UndoOne 的部分成功路径返回 (target, err)，而 app.go 的 undoExecuteItem 在
// 错误分支写的是 `return "", uerr` —— 落点就地扔掉，用户可见的失败清单里
// Path 只有 OrigPath。
//
// §19.3 原计划说"引用新返回值形状的部分由变异 M19-c 提供"，实测**不必**：
// undoExecuteItem 的签名改前改后都是 (string, error)，只是错误分支把第一个值
// 写成了 ""，所以本用例在改前树就能编译并真跑红。这比"靠变异顶红"强，
// 已把这条更正写进 §19.7。
//
// 接缝用 undoOneFn（既有）：让"数据已经落在别处、收尾失败"成为一个确定性的
// 返回对，而不是靠竞态撞出来。错误文本里**故意不含**落点路径 —— 那正是
// undo.go:178 改前的形状，本探针要钉的是"就算文案漏了，app 层也不许再丢一次"。

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filededup/internal/history"
	"filededup/internal/ops"
)

// m86Seeded 备好一条可撤的 trash 账（落点在盘上真实存在），返回账本条目。
func m86Seeded(t *testing.T, a *App, orig, dest string, payload []byte) (int64, history.OpItem) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	opID := seedExecutedOp(t, a, "trash", []string{orig}, []string{dest},
		uint64(len(payload)), [][32]byte{blake3Of(t, dest)}, time.Now().UnixNano())
	_, items, err := a.hist.GetOp(opID)
	if err != nil || len(items) != 1 {
		t.Fatalf("账本条目未备好: n=%d err=%v", len(items), err)
	}
	return opID, items[0]
}

// TestUndoExecuteItemKeepsPartialLanding 结构腿：undoExecuteItem 的错误分支
// 必须把 restored 一起交回去（改前恒为 ""）。
func TestUndoExecuteItemKeepsPartialLanding(t *testing.T) {
	dir := t.TempDir()
	a, _ := newHistApp(t)
	land := filepath.Join(dir, "home", "a.bin")
	_, item := m86Seeded(t, a, filepath.Join(dir, "home", "a.bin"),
		filepath.Join(dir, "trash", "a.bin"), []byte("M86-APP-STRUCT"))

	prev := undoOneFn
	undoOneFn = func(ops.UndoItem) (string, error) {
		return land, errors.New("模拟：数据已放到别处，但收尾动作失败")
	}
	t.Cleanup(func() { undoOneFn = prev })

	got, err := a.undoExecuteItem(a.hist, "trash", item)
	if err == nil {
		t.Fatal("前提自检：动作失败时返回值必须带 error")
	}
	if got != land {
		t.Errorf("部分还原的落点必须在错误分支一起交回去，实测 %q（want %q）——"+
			"app 层把它丢成空串，失败清单就再也没有第二次机会告诉用户数据在哪", got, land)
	}
}

// TestUndoPartialLandingReachesFailureList 用户可见腿：走完整的 UndoOperation，
// 断言 ops:undo:done 里的 FailedItem 文本含落点。
//
// Path 一格**不钉**：它标识的是"哪一条账目失败"（OrigPath），换成落点反而让
// 失败清单对不上记录明细；落点走 Err 这条通道（FailedDrawer.vue 原样渲染 Err）。
func TestUndoPartialLandingReachesFailureList(t *testing.T) {
	dir := t.TempDir()
	a, rec := newHistApp(t)
	land := filepath.Join(dir, "elsewhere", "a.bin")
	opID, _ := m86Seeded(t, a, filepath.Join(dir, "home", "a.bin"),
		filepath.Join(dir, "trash", "a.bin"), []byte("M86-APP-VISIBLE"))

	prev := undoOneFn
	undoOneFn = func(ops.UndoItem) (string, error) {
		return land, errors.New("模拟：数据已放到别处，但收尾动作失败")
	}
	t.Cleanup(func() { undoOneFn = prev })

	if _, err := a.UndoOperation(opID); err != nil {
		t.Fatal(err)
	}
	ev := rec.waitTerminal(t, "undo")
	if ev != "ops:undo:done" {
		t.Fatalf("终止事件 = %s", ev)
	}
	p, ok := rec.lastWith("ops:undo:done")
	if !ok {
		t.Fatal("没有 ops:undo:done 载荷")
	}
	res, ok := p.(UndoResult)
	if !ok {
		t.Fatalf("载荷类型 %T", p)
	}
	if len(res.Failed) != 1 {
		t.Fatalf("失败清单应恰有 1 条，实测 %+v", res.Failed)
	}
	if !strings.Contains(res.Failed[0].Err, land) {
		t.Errorf("失败清单的文案没带上落点 %s，用户只看到一句失败却不知道数据在哪: %q",
			land, res.Failed[0].Err)
	}
}
