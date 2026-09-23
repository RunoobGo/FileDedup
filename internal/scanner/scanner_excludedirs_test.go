// 扫描排除目录（功能1·遍历层）：Filters.ExcludeDirs 必须让命中目录
// **整棵子树不被遍历**，且只影响子树不影响形近兄弟；显式指定的扫描根优先于排除。
package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/model"
)

// mkExcludedTree 按 rel 列表造文件，返回临时根。
func mkExcludedTree(t *testing.T, rels ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, rel := range rels {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func walkedPaths(res *Result) []string {
	out := make([]string, len(res.Files))
	for i, f := range res.Files {
		out[i] = f.Path
	}
	return out
}

func hasPath(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}

func TestWalkExcludeDirsPrunesWholeSubtree(t *testing.T) {
	root := mkExcludedTree(t,
		"keep.txt",
		"sync/a.txt",
		"sync/deep/b.txt",
		"sync/deep/deeper/c.txt",
		"syncx/d.txt", // 形近兄弟：字符串前缀相同但非同一目录
	)
	res := Walk(context.Background(), []string{root}, &model.Filters{
		ExcludeDirs: []string{filepath.Join(root, "sync")},
	}, 4)
	got := walkedPaths(res)
	for _, want := range []string{
		filepath.Join(root, "keep.txt"),
		filepath.Join(root, "syncx", "d.txt"),
	} {
		if !hasPath(got, want) {
			t.Fatalf("排除 sync 误伤兄弟/正常目录: 缺 %v, got %v", want, got)
		}
	}
	syncDir := filepath.Join(root, "sync") + string(filepath.Separator)
	for _, p := range got {
		if filepath.Dir(p) == syncDir[:len(syncDir)-1] || filepath.HasPrefix(p, syncDir) {
			t.Fatalf("排除目录子树仍在结果里: %v（got %v）", p, got)
		}
	}
}

func TestWalkExcludeDirsSparesExplicitRoot(t *testing.T) {
	// 与系统保护清单同一裁定（scanner 目录级保护的既有先例）：用户显式把
	// 目录设为扫描根时，指名优先——排除表不得静默掏空一个被点名的根。
	root := mkExcludedTree(t, "sub/a.txt")
	sub := filepath.Join(root, "sub")
	res := Walk(context.Background(), []string{sub}, &model.Filters{
		ExcludeDirs: []string{sub},
	}, 2)
	got := walkedPaths(res)
	if !hasPath(got, filepath.Join(sub, "a.txt")) {
		t.Fatalf("显式根被排除表剪掉: got %v", got)
	}
}

func TestWalkExcludeDirsEmptyIsNoop(t *testing.T) {
	root := mkExcludedTree(t, "a.txt", "b/c.txt")
	res := Walk(context.Background(), []string{root}, &model.Filters{}, 2)
	if len(res.Files) != 2 {
		t.Fatalf("未配置 ExcludeDirs 时遍历量改变: %d files", len(res.Files))
	}
}
