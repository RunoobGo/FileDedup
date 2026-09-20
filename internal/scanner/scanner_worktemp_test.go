package scanner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/model"
)

// 缺陷 6（2026-09-19）扫描侧回归：应用工作临时文件必须被忽略。
//
// 现场：硬链接合并成功后，原独立副本 (*.fdd-old) 因被杀软/索引器占用而
// 删除失败（旧代码 `_ = os.Remove(...)` 静默吞掉）。它是合并前的完整副本，
// 内容与保留源逐字节相同，且**不以 "." 开头**（隐藏跳过规则无效）——
// 于是每次扫描都把它与保留源配成"重复组"。
// 用户感知即为：「已操作硬链接的文件重扫仍被识别为重复文件」。
//
// 本测试同时钉住两点：这些名字被忽略（不再污染结果），
// 且用户正常文件**不**被误伤（避免漏扫）。
func TestWalkSkipsWorkTempResidue(t *testing.T) {
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

	// 保留源 + 遗留的合并残留（内容相同 → 旧行为下必然成组）
	mk("keep.bin", "identical-payload")
	mk("dup.bin.fdd-old", "identical-payload")      // 合并残留（未删净）
	mk("dup.bin.fdd-tmp", "identical-payload")      // 中断留下的临时硬链接
	mk("dup.bin.fdd-old.undo", "identical-payload") // 回滚暂存
	mk("photo.jpg.fdd-restored.jpg", "identical-payload")
	// AS-R1：原位与 a.fdd-restored.jpg 都被占时，claimDst 把 _N 插在扩展名之前
	mk("photo.jpg.fdd-restored_2.jpg", "identical-payload")

	// 用户正常文件：必须仍被收集，绝不能被误忽略
	mk("real1.bin", "identical-payload")
	mk("real2.bin", "identical-payload")

	res := Walk(context.Background(), []string{root}, &model.Filters{}, 4)

	got := map[string]bool{}
	for _, f := range res.Files {
		got[filepath.Base(f.Path)] = true
	}
	for _, name := range []string{
		"dup.bin.fdd-old", "dup.bin.fdd-tmp", "dup.bin.fdd-old.undo",
		"photo.jpg.fdd-restored.jpg", "photo.jpg.fdd-restored_2.jpg",
	} {
		if got[name] {
			t.Errorf("工作临时文件 %q 不应被扫描收集（会污染重复分组）", name)
		}
	}
	for _, name := range []string{"keep.bin", "real1.bin", "real2.bin"} {
		if !got[name] {
			t.Errorf("用户文件 %q 应被正常收集", name)
		}
	}
	if len(res.Files) != 3 {
		var names []string
		for _, f := range res.Files {
			names = append(names, filepath.Base(f.Path))
		}
		t.Errorf("应恰好收集 3 个用户文件，实际 %d: %v", len(res.Files), names)
	}
}

// TestWalkDoesNotPruneUserDirsNamedLikeTempMarkers 名字以标记结尾的**目录**
// 不是我们的产物（我们只生成文件），不得连整棵子树一起剪掉。
//
// 修正前：IsTempName(de.Name()) 在 IsDir 分支**之前**执行且用 Contains，
// 用户目录 `album.fdd-old-collection/`（乃至任何内嵌标记的目录名）会被
// 静默整树排除——漏扫其中全部文件，比漏单个文件严重。
func TestWalkDoesNotPruneUserDirsNamedLikeTempMarkers(t *testing.T) {
	root := t.TempDir()
	payload := strings.Repeat("k", 100)
	for _, dirName := range []string{"album.fdd-old-collection", "album.fdd-old"} {
		dir := filepath.Join(root, dirName)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, n := range []string{"p1.jpg", "p2.jpg"} {
			if err := os.WriteFile(filepath.Join(dir, n), []byte(payload), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	res := Walk(context.Background(), []string{root}, &model.Filters{}, 4)
	if len(res.Files) != 4 {
		var names []string
		for _, f := range res.Files {
			names = append(names, f.Path)
		}
		t.Fatalf("目录名内嵌/等同临时标记都不得剪树，应收集 4 个文件，实际 %d: %v", len(res.Files), names)
	}
}

// TestWalkSkipsWorkTempInSubdirectories 残留可能出现在任意层级，
// 判定必须与目录深度无关。
func TestWalkSkipsWorkTempInSubdirectories(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "l1", "l2", "l3")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "movie.mkv.fdd-old"),
		[]byte(strings.Repeat("z", 100)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "movie.mkv"),
		[]byte(strings.Repeat("z", 100)), 0o644); err != nil {
		t.Fatal(err)
	}

	res := Walk(context.Background(), []string{root}, &model.Filters{}, 4)
	for _, f := range res.Files {
		if strings.HasSuffix(f.Path, ".fdd-old") {
			t.Fatalf("深层残留未被忽略: %s", f.Path)
		}
	}
	if len(res.Files) != 1 {
		t.Fatalf("应只收集 1 个真实文件，实际 %d", len(res.Files))
	}
}
