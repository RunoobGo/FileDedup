package scanner

// SCN-3（2026-09-21 全量审查，设计稿 §15.0-A）。
//
// rootPrefixes 原先**无条件**给每个根追加一个分隔符。根本身已带尾分隔符时
// （用户从文件管理器复制出来的 "/Volumes/x/Object/"，以及盘根 "/"、"C:\\"）
// 前缀变成双分隔符 ⇒ relativeTo 对该根下的任何文件都匹配不上 ⇒ 走
// `filepath.Base(full)` 兜底 ⇒ 含 "/" 的 ExcludePaths 从此**静默永不命中**：
// 用户写了排除，界面没说错，盘上一个都没排。方向是放行，比"多扫"更糟。
//
// 基准实现 scanner_test.go 里的 relativeTo 参照写法同病（同一处 r+sep），
// 所以等价性用例在结构上抓不到这一条 ⇒ 本文件用真夹具走端到端，不靠等价性比对。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/model"
)

// 主探针一（单元级）：根已带尾分隔符时不得再追加。
func TestRootPrefixesDoNotDoubleSeparator(t *testing.T) {
	sep := string(filepath.Separator)
	cases := []struct {
		root, want string
	}{
		{sep, sep},                       // 盘根（unix "/"、windows "\"）：再追加就成了双分隔符
		{"/tmp/x" + sep, "/tmp/x" + sep}, // 用户从地址栏复制的带尾斜杠根
		{"/tmp/x", "/tmp/x" + sep},       // 常规根：仍须补分隔符（否则 /tmp/xy 会被算进 /tmp/x 内）
	}
	for _, c := range cases {
		got := rootPrefixes([]string{c.root})[0]
		if got != c.want {
			t.Errorf("rootPrefixes(%q) = %q, want %q", c.root, got, c.want)
		}
	}
	if got := relativeTo(rootPrefixes([]string{sep}), sep+"a/b/c.txt"); got != "a/b/c.txt" {
		t.Fatalf("盘根下的 relativeTo = %q, want %q："+
			"落到 filepath.Base 兜底 ⇒ 任何含 %q 的 ExcludePaths 静默失效（§15.0-A SCN-3）",
			got, "a/b/c.txt", "/")
	}
}

// 端到端负控制（**不是**本条缺陷的复现路径，写清楚免得下一轮误读）：
// dedupeRoots 会 Clean 掉尾部斜杠，所以"用户粘贴带尾斜杠的根"在端到端上打不到
// rootPrefixes 的双分隔符分支——真正能打到的只有**盘根**（Clean 后仍是 "/"），
// 而盘根扫描在本机不可行（会把整盘扫一遍）。因此本用例只钉住"排除在普通根上
// 仍然有效"，SCN-3 的红证据是上面的单元探针；盘根那一支属 §15.5 的未兑现边界。
func TestExcludePathsWithTrailingSeparatorRoot(t *testing.T) {
	root := t.TempDir()
	keep := filepath.Join(root, "keep")
	gen := filepath.Join(root, "gen", "data")
	for _, d := range []string{keep, gen} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{filepath.Join(keep, "a.txt"), filepath.Join(gen, "b.txt")} {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res := Walk(context.Background(), []string{root + string(filepath.Separator)},
		&model.Filters{ExcludePaths: []string{"gen/data"}, IncludeHidden: true}, 2)
	for _, e := range res.Files {
		if strings.Contains(e.Path, filepath.Join("gen", "data")) {
			t.Fatalf("根带尾分隔符时 gen/data 的排除失效（共 %d 项）：%v", len(res.Files), e.Path)
		}
	}
	if len(res.Files) != 1 {
		t.Fatalf("应只剩 keep/a.txt，实际 %d 项", len(res.Files))
	}
}

// 负控制：不带尾分隔符时同一套排除本就有效——守卫不得把它改坏。
func TestExcludePathsPlainRootStillWorks(t *testing.T) {
	root := t.TempDir()
	gen := filepath.Join(root, "gen", "data")
	if err := os.MkdirAll(gen, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gen, "b.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := Walk(context.Background(), []string{root},
		&model.Filters{ExcludePaths: []string{"gen/data"}, IncludeHidden: true}, 2)
	if len(res.Files) != 1 || filepath.Base(res.Files[0].Path) != "keep.txt" {
		var got []string
		for _, e := range res.Files {
			got = append(got, e.Path)
		}
		t.Fatalf("普通根的排除被改坏：%v", got)
	}
}
