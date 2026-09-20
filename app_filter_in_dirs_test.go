package main

// AS-H6 / 决策 D-1（方案 b，2026-09-20 全仓审计）：
// 「将处理 N / 已排除 M」的判据从前端收回后端，本文件钉住收回后的**绑定契约**：
// 返回的下标必须正好是「该目录下那些文件」的下标，且这次调用不产生任何副作用。
//
// 期望值刻意用测试侧独立的 dirContains 计算（见 app_process_test.go 上方注释），
// 不复用被测的 ops.inDir —— 否则判据错了测试会跟着一起错。

import (
	"os"
	"path/filepath"
	"testing"
)

// resultPaths 取结果集里全部文件路径（含保留项，按组序/组内序）。
func resultPaths(t *testing.T, a *App) []string {
	t.Helper()
	r, err := a.GetResultGroups(ResultQuery{PageSize: 500})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, g := range r.Groups {
		for _, f := range g.Files {
			out = append(out, f.Path)
		}
	}
	return out
}

// TestFilterInDirsBindingReturnsMatchingIndices 绑定返回的下标必须与
// 「路径落在优先目录（含子目录）下」一一对应。
func TestFilterInDirsBindingReturnsMatchingIndices(t *testing.T) {
	a, _, _, insideDir, outsideDir := procFixture(t)
	paths := resultPaths(t, a)
	if len(paths) < 4 {
		t.Fatalf("夹具应至少产出 4 个路径（keep + inside×2 + outside），实得 %d", len(paths))
	}

	// 正向优先目录：下标集合与独立实现算出的完全一致。
	inside := idsInDir(t, a, insideDir)
	got := a.FilterInDirs([]string{insideDir}, paths)
	var want []int
	for i, p := range paths {
		if dirContains(insideDir, p) {
			want = append(want, i)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("优先目录 %q 命中 %v，应为 %v（路径 %v）", insideDir, got, want, paths)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("命中下标第 %d 位 = %d，应为 %d（%v vs %v）", i, got[i], want[i], got, want)
		}
	}
	// 命中数必须等于「非保留项在 inside 下的个数」：夹具里 keep.bin 在
	// keepme/ 下、outside/c.bin 在 outside/ 下，都不属于 inside。
	if len(got) != len(inside.ids) {
		t.Fatalf("命中 %d 项，应为 %d 项: %v", len(got), len(inside.ids), got)
	}
	for _, i := range got {
		if dirContains(outsideDir, paths[i]) {
			t.Fatalf("范围外文件 %s 被判为命中（目录 %q）", paths[i], insideDir)
		}
	}

	// 多目录是并集。
	two := a.FilterInDirs([]string{insideDir, outsideDir}, paths)
	if len(two) != len(got)+1 {
		t.Fatalf("并入 outside 后应多命中 1 项，实得 %v（此前 %v）", two, got)
	}
	// 目录写法的变体必须给同一个答案（前端那份实现栽在这里：不做 Clean 归一）。
	for _, variant := range []string{insideDir + string(filepath.Separator), "  " + insideDir + "  "} {
		v := a.FilterInDirs([]string{variant}, paths)
		if len(v) != len(got) {
			t.Fatalf("目录变体 %q 命中 %v，应与 %q 一致 %v", variant, v, insideDir, got)
		}
	}
}

// TestFilterInDirsBindingDisabled 未启用（dirs 空/全空白）→ 空列表，
// 前端据此走"不过滤"的原路径；绝不能实现成"全部命中"。
func TestFilterInDirsBindingDisabled(t *testing.T) {
	a, _, _, _, _ := procFixture(t)
	paths := resultPaths(t, a)
	for _, dirs := range [][]string{nil, {}, {""}, {"   "}} {
		if got := a.FilterInDirs(dirs, paths); len(got) != 0 {
			t.Fatalf("目录 %q 应视为未启用，实得 %v", dirs, got)
		}
	}
}

// TestFilterInDirsBindingHasNoSideEffects 只读：不动文件、不置 opsRunning、
// 不改结果集，也不受 opsRunning 互斥限制（判据是纯字符串计算，不该被在途清理挡住）。
func TestFilterInDirsBindingHasNoSideEffects(t *testing.T) {
	a, _, _, insideDir, _ := procFixture(t)
	paths := resultPaths(t, a)

	a.mu.Lock()
	a.opsRunning = true
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.opsRunning = false
		a.mu.Unlock()
	}()

	got := a.FilterInDirs([]string{insideDir}, paths)
	if len(got) == 0 {
		t.Fatal("清理进行中调用被挡住了：判据是纯计算，不该受 opsRunning 限制")
	}

	a.mu.Lock()
	running := a.opsRunning
	a.mu.Unlock()
	if !running {
		t.Fatal("绑定不该改写 opsRunning")
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("绑定不该动文件: %s err=%v", p, err)
		}
	}
	r, err := a.GetResultGroups(ResultQuery{PageSize: 500})
	if err != nil {
		t.Fatal(err)
	}
	if len(resultPaths(t, a)) != len(paths) {
		t.Fatalf("绑定不该改动结果集（前 %d 后 %d）", len(paths), len(r.Groups))
	}
}
