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
	"filededup/internal/pathnorm"
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
// M36（2026-09-21）之后这里是折叠**唯一**的消费方（遍历键一律不折，见 visitKey），
// 故这条也是"折叠没有一并被删掉"的反向对照。
func TestDedupeRootsFoldsByProbedVolume(t *testing.T) {
	t.Cleanup(func() { probeCaseVerdict = fscase.Verdict })
	base := t.TempDir() // 绝对路径，分隔符与卷名前缀交给平台
	variants := []string{filepath.Join(base, "a"), filepath.Join(base, "A")}

	var probed atomic.Int32
	sens := func(v bool) {
		probed.Store(0)
		probeCaseVerdict = func(string) fscase.Result {
			probed.Add(1)
			return fscase.Result{Sensitive: v, Proven: true}
		}
	}

	sens(false)
	kept, _, _ := dedupeRoots(variants)
	if len(kept) != 1 {
		t.Fatalf("探测说不敏感时保留根数 = %d, want 1（两根应被并成一棵）：%v", len(kept), kept)
	}
	if n := probed.Load(); n != 2 {
		t.Fatalf("两根卷探测调用 = %d, want 2", n)
	}

	sens(true)
	kept, _, _ = dedupeRoots(variants)
	if len(kept) != 2 {
		t.Fatalf("探测说敏感时保留根数 = %d, want 2（两棵子树各自入列，修正前被折叠成一棵）：%v",
			len(kept), kept)
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
	t.Cleanup(func() { probeCaseVerdict = fscase.Verdict })
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
		probeCaseVerdict = func(string) fscase.Result {
			probed.Add(1)
			return fscase.Result{Sensitive: v, Proven: true}
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
	t.Cleanup(func() { probeCaseVerdict = fscase.Verdict })
	root := t.TempDir()
	mkDirFiles(t, root, "x.txt")
	var probed atomic.Int32
	probeCaseVerdict = func(string) fscase.Result {
		probed.Add(1)
		return fscase.Result{Sensitive: true, Proven: true}
	}
	if res := Walk(context.Background(), []string{root}, &model.Filters{}, 2); len(res.Files) != 1 {
		t.Fatalf("文件数 = %d, want 1", len(res.Files))
	}
	if n := probed.Load(); n != 0 {
		t.Fatalf("单根扫描触发了 %d 次卷探测，应为 0", n)
	}
}

// TestVisitKeyNeverFolds M36（设计稿 §10.3 V1）：**遍历期的比较键一律不折叠**。
//
// 这里原位替换了原先三条直接戳 folder 的用例——`TestFolderSingleRootNeverFolds`、
// `TestFolderMultiRootStillFolds`、`TestFolderFoldPerRoot`。它们断言的对象
// （`newFolder`/`fold`/`foldRoot`/`needFold`）在方案 C 下被整体删除，属设计稿 §6.2
// 已经预告过的"推翻 I2 当年定的机制"那一半；旧用例要的"折叠按各根所在卷分别生效"
// 现在**只**发生在 `dedupeRoots` 的合并判据里，由上面的
// TestDedupeRootsFoldsByProbedVolume 继续钉住。遍历键这边的新判据更强：
// 不分根数、不看卷语义、一律拼写精确（三个维度都不再有参数）。
//
// 纯字符串推理，不碰盘、不需要敏感卷，故在任何平台任何卷上都成立；
// 真夹具与真构型（hdiutil 挂敏感卷）读数见同目录 scanner_m36_test.go 与设计稿 §10.0。
func TestVisitKeyNeverFolds(t *testing.T) {
	sep := string(filepath.Separator)
	toSlash := func(p string) string { return strings.ReplaceAll(p, sep, "/") }

	// 1) 大小写原样：路径里有几处大小写，键里就有几处（只允许分隔符归一）。
	p := filepath.Join(sep, "X", "Docs", "Sub", "F.TXT")
	if got := visitKey(p); got != toSlash(p) {
		t.Errorf("visitKey(%q) = %q, want %q（键只允许归一分隔符）", p, got, toSlash(p))
	}
	// 2) 反向：两种拼写必须给出两个键——这正是 M36 的判据本身
	//    （折成一个键 ⇒ 大小写不同的两棵子树被并成一棵，静默漏扫且不记失败）。
	lower := strings.ToLower(p)
	if visitKey(lower) == visitKey(p) {
		t.Errorf("visitKey 把两种拼写折成同一个键（%q）：大小写不同的两棵子树会被并成一棵",
			visitKey(p))
	}
	// 3) 键空间约定不变（M26）：分隔符归一到 "/"，与平台无关。
	if got := pathnorm.Slash(`C:\a\B`, `\`); got != "C:/a/B" {
		t.Errorf("Slash 在 Windows 真值下未把分隔符归一到键空间：%q", got)
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
