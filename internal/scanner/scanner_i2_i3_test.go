package scanner

// 2026-09-18 审查 I2 / I3 回归：
//   - I2：折叠决策来自按卷探测，不再是编译目标的硬编码假定；
//   - I3：遍历单个目录 panic 只记 Failed，不得带走整个进程。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"filededup/internal/fscase"
	"filededup/internal/model"
)

// panicGate 在第 panicOn 次 Wait 时 panic（Wait 位于目录处理内，等价于该目录炸了）。
type panicGate struct {
	calls   atomic.Int32
	panicOn int32
}

func (g *panicGate) Wait(ctx context.Context) error {
	if g.calls.Add(1) == g.panicOn {
		panic("boom：底层返回了畸形 DirEntry")
	}
	return ctx.Err()
}

func mkDirFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("payload-"+n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestWalkPanicIsolatedAsFailed 首个子目录 panic：进程必须活着、该目录记 Failed、
// 其余目录照常收齐，且 Walk 正常返回（在途计数不得因 panic 漏销而永久挂住）。
// 修正前：worker 无 recover，panic 直接击穿 goroutine → 整个进程退出。
func TestWalkPanicIsolatedAsFailed(t *testing.T) {
	root := t.TempDir()
	mkDirFiles(t, root, "r.txt")
	for _, d := range []string{"d0", "d1", "d2"} {
		mkDirFiles(t, filepath.Join(root, d), d+".txt")
	}
	g := &panicGate{panicOn: 2} // 第 1 次=root，第 2 次=d0（单 worker + FIFO，确定）

	done := make(chan *Result, 1)
	go func() {
		done <- WalkWithGate(context.Background(), []string{root}, &model.Filters{}, 1, g)
	}()
	var res *Result
	select {
	case res = <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("panic 后 Walk 未返回（在途目录计数疑似漏销）")
	}

	if len(res.Failed) != 1 {
		t.Fatalf("Failed 条数 = %d, want 1（%+v）", len(res.Failed), res.Failed)
	}
	fi := res.Failed[0]
	if fi.Stage != "scan" || filepath.Base(fi.Path) != "d0" ||
		!strings.Contains(fi.Err, "遍历异常已隔离") {
		t.Fatalf("Failed 记录失真: %+v", fi)
	}
	got := make([]string, 0, len(res.Files))
	for _, f := range res.Files {
		got = append(got, filepath.Base(f.Path))
	}
	want := map[string]bool{"r.txt": true, "d1.txt": true, "d2.txt": true}
	if len(got) != len(want) {
		t.Fatalf("panic 后收齐文件 = %v, want r.txt/d1.txt/d2.txt", got)
	}
	for _, n := range got {
		if !want[n] {
			t.Fatalf("多出文件 %q（实际 %v）", n, got)
		}
	}
}

// TestWalkCaseSensitivityIsProbed 同一对「仅大小写不同的根」在敏感卷与不敏感卷
// 上必须给出不同结论：不敏感卷并成一棵（收一次），敏感卷视为两棵（收两次）。
// 修正前只有编译目标钦定的一条路：macOS 上敏感卷会静默漏扫一棵子树。
func TestWalkCaseSensitivityIsProbed(t *testing.T) {
	t.Cleanup(func() { probeCaseSensitive = fscase.Sensitive })
	root := t.TempDir()
	a := filepath.Join(root, "a")
	mkDirFiles(t, a, "x.txt", "y.txt")
	variants := []string{a, strings.ToUpper(a)} // 不敏感卷上指向同一目录

	var probed atomic.Int32
	sens := func(v bool) {
		probeCaseSensitive = func(string) bool {
			probed.Add(1)
			return v
		}
	}

	sens(false)
	ins := Walk(context.Background(), variants, &model.Filters{}, 2)
	if len(ins.Files) != 2 {
		t.Fatalf("不敏感卷折叠后文件数 = %d, want 2（两根被并成一棵才有 2）", len(ins.Files))
	}
	if n := probed.Load(); n != 2 {
		t.Fatalf("两根卷探测调用 = %d, want 2", n)
	}

	sens(true)
	probed.Store(0)
	sec := Walk(context.Background(), variants, &model.Filters{}, 2)
	if len(sec.Files) != 4 {
		t.Fatalf("敏感卷文件数 = %d, want 4（两棵子树各自入列，修正前被折叠成一棵）", len(sec.Files))
	}
	if n := probed.Load(); n == 0 {
		t.Fatal("敏感卷未走按卷探测（折叠仍被硬编码）")
	}
}

// TestWalkSingleRootSkipsProbe 单根无从判重，不该往用户目录里写探测文件。
func TestWalkSingleRootSkipsProbe(t *testing.T) {
	t.Cleanup(func() { probeCaseSensitive = fscase.Sensitive })
	root := t.TempDir()
	mkDirFiles(t, root, "x.txt")
	var probed atomic.Int32
	probeCaseSensitive = func(string) bool {
		probed.Add(1)
		return true
	}
	if res := Walk(context.Background(), []string{root}, &model.Filters{}, 2); len(res.Files) != 1 {
		t.Fatalf("文件数 = %d, want 1", len(res.Files))
	}
	if n := probed.Load(); n != 0 {
		t.Fatalf("单根扫描触发了 %d 次卷探测，应为 0", n)
	}
}

// TestFolderFoldPerRoot 混合卷语义：折叠按各根所在卷分别生效，不能一刀切。
func TestFolderFoldPerRoot(t *testing.T) {
	roots := []string{"/sen", "/ins"}
	f := newFolder(roots, []bool{true, false})
	if got := f.fold("/sen/Dir/F.TXT"); got != "/sen/Dir/F.TXT" {
		t.Errorf("敏感卷根下的路径被折叠了: %q", got)
	}
	if got := f.fold("/ins/Dir/F.TXT"); got != "/ins/dir/f.txt" {
		t.Errorf("不敏感卷根下的路径未折叠: %q", got)
	}
	if got := f.foldRoot(0); got != "/sen" {
		t.Errorf("foldRoot(0) = %q", got)
	}
	if got := f.foldRoot(1); got != "/ins" {
		t.Errorf("foldRoot(1) = %q", got)
	}
	// 兜底：不属于任何根的路径按平台默认语义
	const stray = "/other/MIX.txt"
	want := strings.ToLower(stray)
	if fscase.Default() {
		want = stray
	}
	if got := f.fold(stray); got != want {
		t.Errorf("fold(%q) = %q, want %q（默认语义=%v）", stray, got, want, fscase.Default())
	}
}
