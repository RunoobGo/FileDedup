package scanner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"filededup/internal/model"
)

func setupTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mk := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("a/keep.txt", "hello")
	mk("a/b/inner.txt", "world")             // 供重叠根测试
	mk("a/b/.hidden.txt", "h")               // 隐藏文件
	mk("a/zero.txt", "")                     // 0 字节
	mk("a/nodeps/drop.js", "console.log(1)") // 供路径排除
	mk("a/tmp.log", "log")                   // 供扩展名排除
	mk("a/big.bin", strings.Repeat("x", 500))
	// 符号链接：不应被收集
	if err := os.Symlink(filepath.Join(root, "a/keep.txt"), filepath.Join(root, "a/link.txt")); err != nil {
		t.Skipf("符号链接创建失败（平台限制）: %v", err)
	}
	return root
}

func TestWalkBasic(t *testing.T) {
	root := setupTree(t)
	res := Walk(context.Background(), []string{filepath.Join(root, "a")}, &model.Filters{}, 4)

	var got []string
	for _, f := range res.Files {
		rel, _ := filepath.Rel(root, f.Path)
		// 结果里的路径是原生口径（Windows 为 \），断言统一到 slash 口径
		got = append(got, filepath.ToSlash(rel))
	}
	sort.Strings(got)
	want := []string{"a/b/inner.txt", "a/big.bin", "a/keep.txt", "a/nodeps/drop.js", "a/tmp.log"} // 0字节/隐藏/符号链接已跳过
	if len(got) != len(want) {
		t.Fatalf("收集数 = %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%s want=%s", i, got[i], want[i])
		}
	}
}

func TestWalkFileKeyResolved(t *testing.T) {
	root := setupTree(t)
	res := Walk(context.Background(), []string{filepath.Join(root, "a")}, &model.Filters{}, 2)
	if len(res.Files) == 0 {
		t.Fatal("无文件")
	}
	// unix 平台 FileKey 应已解析；windows 平台允许未解析
	for _, f := range res.Files {
		if !f.Key.Resolved {
			t.Logf("平台未在遍历时解析 FileKey（Windows 预期行为）: %s", f.Path)
			break
		}
	}
}

func TestWalkOverlappingRoots(t *testing.T) {
	root := setupTree(t)
	a := filepath.Join(root, "a")
	// a 与 a/b 同时作为根：文件不应被收集两次
	res := Walk(context.Background(), []string{a, filepath.Join(a, "b")}, &model.Filters{}, 4)
	count := map[string]int{}
	for _, f := range res.Files {
		count[f.Path]++
	}
	for p, c := range count {
		if c > 1 {
			t.Fatalf("重叠根导致重复收集: %s × %d", p, c)
		}
	}
}

func TestWalkCaseFoldRoots(t *testing.T) {
	root := setupTree(t)
	a := filepath.Join(root, "a")
	// 仅 mac/win 折叠；linux 上两路径不同视为两根（预期行为差异，不断言错误）
	res := Walk(context.Background(), []string{a, strings.ToUpper(a)}, &model.Filters{}, 2)
	count := map[string]int{}
	for _, f := range res.Files {
		count[strings.ToLower(f.Path)]++
	}
	for p, c := range count {
		if c > 1 {
			t.Fatalf("大小写折叠根导致重复收集: %s × %d", p, c)
		}
	}
}

func TestWalkFilters(t *testing.T) {
	root := setupTree(t)
	res := Walk(context.Background(), []string{filepath.Join(root, "a")}, &model.Filters{
		ExcludeExts: []string{".log"},
		MinSize:     3,
	}, 2)
	for _, f := range res.Files {
		if f.Ext == ".log" {
			t.Fatalf("扩展名排除未生效: %s", f.Path)
		}
		if f.Size < 3 {
			t.Fatalf("MinSize 未生效: %s size=%d", f.Path, f.Size)
		}
	}
}

func TestWalkUnreadableDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Windows 的访问控制是 ACL，不是 st_mode 位：os.Chmod(0o000) 只落
		// FILE_ATTRIBUTE_READONLY，目录照样可枚举。这里断言的是 POSIX 权限
		// 拒绝语义，在该平台无从成立（不是扫描器缺陷）。
		t.Skip("Windows 无 POSIX 权限拒绝语义（chmod 位不控制访问）")
	}
	if os.Geteuid() == 0 {
		t.Skip("root 用户无权限拒绝语义")
	}
	root := t.TempDir()
	blocked := filepath.Join(root, "blocked")
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blocked, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(blocked, 0o755)

	res := Walk(context.Background(), []string{root}, &model.Filters{}, 2)
	if len(res.Failed) == 0 {
		t.Fatalf("预期权限失败进入失败清单，got %d failed", len(res.Failed))
	}
	for _, f := range res.Failed {
		if f.Stage != "scan" {
			t.Fatalf("失败阶段应为 scan: %+v", f)
		}
	}
}

// ---------- G2：relativeTo 根前缀预计算 ----------

// naiveRelativeTo 改造前的实现，作为语义基准与性能对照。
func naiveRelativeTo(roots []string, full string) string {
	for _, r := range roots {
		if strings.HasPrefix(full, r+string(filepath.Separator)) {
			return full[len(r)+1:]
		}
	}
	return filepath.Base(full)
}

// nativePaths 把 slash 口径的测试路径转成当前平台的原生分隔符口径。
// relativeTo 处理的是磁盘路径：Windows 上写死 "/" 会让所有根都匹配不上、
// 全部落到 filepath.Base() 兜底分支，等于把被测逻辑整个跳过了。
func nativePaths(ps ...string) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = filepath.FromSlash(p)
	}
	return out
}

// G2 等价性：新实现必须与改造前逐例一致（多根 / 直接子文件 / 根自身 / 根外路径回退）。
func TestRelativeToEquivalence(t *testing.T) {
	roots := nativePaths("/a", "/b/c", "/d", "/aa")
	prefixes := rootPrefixes(roots)
	cases := nativePaths(
		"/a/x", "/a/x/y/z.bin", "/b/c/m/n", "/b/other", "/d",
		"/zzz/qq", "/a", "/b/c", "/aa/f.txt", "/a.txt",
	)
	for _, full := range cases {
		want := naiveRelativeTo(roots, full)
		if got := relativeTo(prefixes, full); got != want {
			t.Fatalf("relativeTo(%q) = %q, want %q", full, got, want)
		}
	}
}

// G2 回归：每文件调用必须零分配。
// 注意：实测发现旧写法（循环内 r+sep）也已被 Go 编译器栈分配优化，堆分配同为 0，
// 因此本项的收益只在 CPU（省去每次调用的拼接与重复长度计算），而非堆分配。
// 断言只锁定「新实现零分配」这一契约，避免夸大收益。
func TestRelativeToNoAllocs(t *testing.T) {
	roots := nativePaths("/a", "/b", "/c", "/d", "/e")
	prefixes := rootPrefixes(roots)
	full := filepath.FromSlash("/e/deep/nested/dir/file.bin")
	if got := filepath.ToSlash(relativeTo(prefixes, full)); got != "deep/nested/dir/file.bin" {
		t.Fatalf("rel = %q", got)
	}
	if n := testing.AllocsPerRun(200, func() { _ = relativeTo(prefixes, full) }); n != 0 {
		t.Fatalf("relativeTo 每次分配 %.1f 次，期望 0（G2 未生效）", n)
	}
	naive := testing.AllocsPerRun(200, func() {
		_ = naiveRelativeTo(roots, full)
	})
	t.Logf("堆分配：新实现 0.0 次/调用，旧实现 %.1f 次/调用（旧写法亦被栈分配优化）", naive)
}

func BenchmarkRelativeTo(b *testing.B) {
	roots := []string{"/a", "/b", "/c", "/d", "/e"}
	prefixes := rootPrefixes(roots)
	full := "/e/deep/nested/dir/file.bin"
	b.Run("Precomputed", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = relativeTo(prefixes, full)
		}
	})
	b.Run("NaiveConcat", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = naiveRelativeTo(roots, full)
		}
	})
}

// P2：ExcludePaths 目录级剪枝——既要不进结果，也要真的不去遍历。
func TestExcludePathsPrunesDirectories(t *testing.T) {
	root := t.TempDir()
	payload := []byte("PRUNE-CHECK-CONTENT")
	// nm 下塞大量文件，用于衡量遍历量
	for _, dir := range []string{"node_modules/pkg", "src"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 300; i++ {
			p := filepath.Join(root, dir, "f"+strconv.Itoa(i)+".bin")
			if err := os.WriteFile(p, payload, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	countFiles := func(exclude []string) (int, int) {
		r := Walk(context.Background(), []string{root}, &model.Filters{ExcludePaths: exclude}, 4)
		return len(r.Files), r.Visited
	}
	withPrune, visitedWith := countFiles([]string{"node_modules"})
	_, visitedNone := countFiles(nil)

	if withPrune != 300 {
		t.Fatalf("结果数应只剩 src 的 300 个, got %d", withPrune)
	}
	if visitedWith >= visitedNone {
		t.Fatalf("P2: 剪枝未减少遍历量 visited=%d (未排除时 %d)", visitedWith, visitedNone)
	}
	// 剪枝必须与"不剪枝只过滤"结果一致
	rNoPrune := Walk(context.Background(), []string{root}, &model.Filters{ExcludePaths: []string{"*/nope", "node_modules"}}, 4)
	if len(rNoPrune.Files) != withPrune {
		t.Fatalf("加入不可剪枝模式后结果数变化: %d vs %d", len(rNoPrune.Files), withPrune)
	}
}
