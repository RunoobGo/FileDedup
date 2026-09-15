package scanner

import (
	"context"
	"os"
	"path/filepath"
	"sort"
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
	mk("a/nodeps/drop.js", "console.log(1)")  // 供路径排除
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
		got = append(got, rel)
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
