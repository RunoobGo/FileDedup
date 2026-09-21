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

// TestDedupeRootsFoldsByProbedVolume 折叠判定的平台无关版本：只用字符串推理，
// 不碰盘，因此在任何分隔符、任何大小写语义的卷上都成立。
// 断言的是 I2 的实质——折叠与否**只来自按卷探测**：
// 同一对仅大小写不同的根，探测说不敏感就并成一棵，说敏感就留两棵。
func TestDedupeRootsFoldsByProbedVolume(t *testing.T) {
	t.Cleanup(func() { probeCaseSensitive = fscase.Sensitive })
	base := t.TempDir() // 绝对路径，分隔符与卷名前缀交给平台
	variants := []string{filepath.Join(base, "a"), filepath.Join(base, "A")}

	var probed atomic.Int32
	sens := func(v bool) {
		probed.Store(0)
		probeCaseSensitive = func(string) bool {
			probed.Add(1)
			return v
		}
	}

	sens(false)
	kept, ksens, _ := dedupeRoots(variants)
	if len(kept) != 1 {
		t.Fatalf("探测说不敏感时保留根数 = %d, want 1（两根应被并成一棵）：%v", len(kept), kept)
	}
	if n := probed.Load(); n != 2 {
		t.Fatalf("两根卷探测调用 = %d, want 2", n)
	}

	sens(true)
	kept, ksens, _ = dedupeRoots(variants)
	if len(kept) != 2 {
		t.Fatalf("探测说敏感时保留根数 = %d, want 2（两棵子树各自入列，修正前被折叠成一棵）：%v",
			len(kept), kept)
	}
	if len(ksens) != 2 || !ksens[0] || !ksens[1] {
		t.Fatalf("保留根的卷语义标注 = %v, want [true true]", ksens)
	}
	if n := probed.Load(); n == 0 {
		t.Fatal("敏感卷未走按卷探测（折叠仍被硬编码）")
	}
}

// TestWalkCaseSensitivityIsProbed 端到端版本：两种拼写各自建成真目录时，
// 探测结果必须真的改变收文件的数量（不敏感并一棵收 2 个，敏感两棵收 4 个）。
// 需要所在卷区分大小写，否则两根无法并存——那是卷的属性，不是被测代码的行为，
// 此时跳过并由上面的字符串层用例覆盖。修正前只有编译目标钦定的一条路：
// macOS 上敏感卷会静默漏扫一棵子树。
func TestWalkCaseSensitivityIsProbed(t *testing.T) {
	t.Cleanup(func() { probeCaseSensitive = fscase.Sensitive })
	root := t.TempDir()
	lower := filepath.Join(root, "a")
	upper := filepath.Join(root, "A")
	if !fscase.Sensitive(root) {
		t.Skipf("临时目录所在卷不区分大小写（%s）：仅大小写不同的两根无法并存", root)
	}
	// 两棵子树内容同名同量，收几次只取决于根是否被判为同一棵。
	mkDirFiles(t, lower, "x.txt", "y.txt")
	mkDirFiles(t, upper, "x.txt", "y.txt")
	variants := []string{lower, upper}

	var probed atomic.Int32
	sens := func(v bool) {
		probed.Store(0)
		probeCaseSensitive = func(string) bool {
			probed.Add(1)
			return v
		}
	}

	sens(false)
	ins := Walk(context.Background(), variants, &model.Filters{}, 2)
	if len(ins.Files) != 2 {
		t.Fatalf("不敏感卷折叠后文件数 = %d, want 2（两根被并成一棵才有 2）：%v",
			len(ins.Files), filePaths(ins.Files))
	}
	if n := probed.Load(); n != 2 {
		t.Fatalf("两根卷探测调用 = %d, want 2", n)
	}

	sens(true)
	sec := Walk(context.Background(), variants, &model.Filters{}, 2)
	if len(sec.Files) != 4 {
		t.Fatalf("敏感卷文件数 = %d, want 4（两棵子树各自入列）：%v",
			len(sec.Files), filePaths(sec.Files))
	}
	if n := probed.Load(); n == 0 {
		t.Fatal("敏感卷未走按卷探测（折叠仍被硬编码）")
	}
}

func filePaths(entries []*model.FileEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Path)
	}
	return out
}

// TestWalkSingleRootSkipsProbe 单根不往用户目录里写探测文件。
// C1（2026-09-21）：**理由已修，断言不变**——此前写的是"单根无从判重，也就不必探测"，
// 但单根扫描照样要用折叠键（visited 去重），它只是**不需要**折叠（单根遍历里每个目录
// 只会以唯一拼写出现，设计稿 §6.1）。折叠因此对单根整个关掉（见 TestFolderSingleRootNeverFolds），
// 于是探测也确实没有存在意义了。
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

// TestFolderSingleRootNeverFolds C1（设计稿 §6.4 V1）：**单根不折叠**是结构性结论
// （单根遍历里每个目录只会以唯一拼写出现），不是"这台机器恰好如此"，故直接对 newFolder
// 断言、不依赖任何卷属性。sens 故意给 false（darwin/windows 的平台默认）——修前正是它
// 让单根的键全部小写化，把被折叠的两棵**不同**目录并成一棵。
func TestFolderSingleRootNeverFolds(t *testing.T) {
	sep := string(filepath.Separator)
	root := filepath.Join(sep, "X", "Docs")
	f := newFolder([]string{root}, []bool{false})
	if f.needFold {
		t.Fatal("单根扫描不应启用折叠：启用后大小写不同的两棵子树会被并成一棵（静默漏扫）")
	}
	sub := filepath.Join(root, "Sub", "F.TXT")
	if got := f.fold(sub); got != sub {
		t.Errorf("单根 fold(%q) = %q, want 原样（改大小写即折叠没关掉）", sub, got)
	}
	if got := f.foldRoot(0); got != root {
		t.Errorf("单根 foldRoot(0) = %q, want %q（visited 种子键必须与 fold 同口径）", got, root)
	}
}

// TestFolderMultiRootStillFolds C1（设计稿 §6.4 V2）：反向对照——两根时 I2 的按卷折叠
// 一字不动，证明修的是"单根"而不是"取消折叠"。
func TestFolderMultiRootStillFolds(t *testing.T) {
	sep := string(filepath.Separator)
	senRoot := filepath.Join(sep, "sen")
	insRoot := filepath.Join(sep, "ins")
	f := newFolder([]string{senRoot, insRoot}, []bool{true, false})
	if !f.needFold {
		t.Fatal("两根且有一根不敏感时必须启用折叠（I2 语义不得被 C1 改动）")
	}
	p := filepath.Join(insRoot, "Dir", "F.TXT")
	want := strings.ToLower(strings.ReplaceAll(p, sep, "/"))
	if got := f.fold(p); got != want {
		t.Errorf("两根时不敏感根下的路径仍应折叠: got %q, want %q", got, want)
	}
}

// TestWalkSingleRootKeepsCaseVariantSubtrees C1 端到端（设计稿 §6.4 V3）：
// alpha/ 与 ALPHA/ 是两棵**不同**子树，单根扫两棵都要收齐。
// 夹具需要所在卷区分大小写（否则两个名字无法并存），没有就跳过——本机 APFS 默认卷
// 与 CI 都会跳过，真读数由设计稿 §6.0 E3/E5 的 CLI 跑取（用 hdiutil 临时挂一棵
// Case-sensitive APFS，不需要 root）。
func TestWalkSingleRootKeepsCaseVariantSubtrees(t *testing.T) {
	root := t.TempDir()
	if !fscase.Sensitive(root) {
		t.Skipf("临时目录所在卷不区分大小写（%s）：alpha/ 与 ALPHA/ 无法并存", root)
	}
	mkDirFiles(t, filepath.Join(root, "alpha"), "a.txt")
	mkDirFiles(t, filepath.Join(root, "ALPHA"), "b.txt")
	res := Walk(context.Background(), []string{root}, &model.Filters{}, 2)
	if len(res.Files) != 2 {
		t.Fatalf("单根扫敏感卷收文件数 = %d, want 2（修前 1：后遇到的那棵子树被折叠丢掉，且不记失败）: %v",
			len(res.Files), filePaths(res.Files))
	}
	if len(res.Failed) != 0 {
		t.Fatalf("不应有失败项（修前也是 0——这正是本项「静默」的地方）: %+v", res.Failed)
	}
}

// TestFolderFoldPerRoot 混合卷语义：折叠按各根所在卷分别生效，不能一刀切。
// 路径一律用 filepath.Join 现拼：fold 靠「root + 原生分隔符」认根，写死 "/" 的用例
// 在 Windows 上会因认不出根而落到平台默认，测到的不是被测的那条分支。
// 折叠键只在遍历内部判重用，不作为对外路径返回，故 fold 允许归一分隔符；
// 断言盯的是「敏感卷不动大小写」这一条。
func TestFolderFoldPerRoot(t *testing.T) {
	sep := string(filepath.Separator)
	toSlash := func(p string) string { return strings.ReplaceAll(p, sep, "/") }
	senRoot := filepath.Join(sep, "sen")
	insRoot := filepath.Join(sep, "ins")
	f := newFolder([]string{senRoot, insRoot}, []bool{true, false})

	senPath := filepath.Join(senRoot, "Dir", "F.TXT")
	insPath := filepath.Join(insRoot, "Dir", "F.TXT")
	if got, want := f.fold(senPath), toSlash(senPath); got != want {
		t.Errorf("敏感卷根下的路径被改了大小写: %q, want %q（只允许分隔符归一）", got, want)
	}
	if got, want := f.fold(insPath), strings.ToLower(toSlash(insPath)); got != want {
		t.Errorf("不敏感卷根下的路径未折叠: %q, want %q", got, want)
	}
	if got, want := f.foldRoot(0), toSlash(senRoot); got != want {
		t.Errorf("foldRoot(0) = %q, want %q", got, want)
	}
	if got, want := f.foldRoot(1), toSlash(insRoot); got != want {
		t.Errorf("foldRoot(1) = %q, want %q", got, want)
	}
	// 兜底：不属于任何根的路径按平台默认语义
	stray := filepath.Join(sep, "other", "MIX.txt")
	want := strings.ToLower(toSlash(stray))
	if fscase.Default() {
		want = toSlash(stray)
	}
	if got := f.fold(stray); got != want {
		t.Errorf("fold(%q) = %q, want %q（默认语义=%v）", stray, got, want, fscase.Default())
	}
}
