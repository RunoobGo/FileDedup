package scanner

// M319（SCN-17，2026-09-28 第六轮全量审查）：worktemp 跳过排在 B5 的 DT_UNKNOWN
// 目录兜底**之前**，于是那道兜底在"临时命名的目录"上永远轮不到。
//
// 形状：`if !typ.IsDir() && worktemp.IsTempName(de.Name())` 里，`typ` 取自 `de.Type()`，
// 而 Type() 对「常规文件」与「d_type 未知项」**都返回 0**（B5 记的就是这件事）。
// FUSE/SMB/部分网络挂载不回填 d_type ⇒ 一个叫 `album.fdd-old` 的用户目录被当成
// "不是目录"，在派发那一刻就 continue 掉：**整棵子树静默丢失，还被计入 wtSkipped**
// （跳过计数说谎：它数的是"到达的文件项"）。M1 那批只补了文件腿的 IsDir 前置，
// B5 之后没有把这条判据重新排一遍。
//
// 判据：修前红可得——夹具与 B5 同源（readDirFn 缝 + unknownTypeEntry），
// 改前 inner.txt 不在结果集且 SkippedWorkTempFiles=1 ⇒ 两处都红；改后子树进入、计数归零。
// 无 build tag：与 B5 同一注入缝，三腿同判据（H6 惯例）。

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestM319DTUnknownWorkTempNamedDirStillTraversed(t *testing.T) {
	root := t.TempDir()
	// 目录名恰为已注册形态（后缀式 `.fdd-old`）：真有人这么命名相册/合集目录。
	sub := filepath.Join(root, "album.fdd-old")
	mkDirFiles(t, sub, "inner.txt")

	orig := readDirFn
	t.Cleanup(func() { readDirFn = orig })
	readDirFn = func(name string) ([]fs.DirEntry, error) {
		ents, err := os.ReadDir(name)
		if err != nil {
			return nil, err
		}
		if filepath.Clean(name) != filepath.Clean(root) {
			return ents, nil
		}
		out := make([]fs.DirEntry, len(ents))
		for i, e := range ents {
			if e.Name() == "album.fdd-old" {
				out[i] = unknownTypeEntry{e} // d_type 未知：派发前看不出是目录
			} else {
				out[i] = e
			}
		}
		return out, nil
	}

	res := Walk(context.Background(), []string{root}, nil, 2)

	want := filepath.Join(sub, "inner.txt")
	found := false
	for _, p := range filePaths(res.Files) {
		if filepath.Clean(p) == filepath.Clean(want) {
			found = true
		}
	}
	if !found {
		t.Fatalf("DT_UNKNOWN 的临时命名目录 %s 整棵子树被静默漏扫：files=%v，want 含 %s",
			"album.fdd-old", namesIn(res), want)
	}
	// 目录本身不是"被跳过的残留文件"：计数说谎与漏扫是同一格的两面。
	if res.SkippedWorkTempFiles != 0 {
		t.Errorf("SkippedWorkTempFiles = %d, want 0（临时命名的目录不得计入，它不是残留文件）",
			res.SkippedWorkTempFiles)
	}
	for _, fi := range res.Failed {
		if filepath.Clean(fi.Path) == filepath.Clean(sub) {
			t.Errorf("该目录不应记 Failed（正常遍历到的目录不是失败）: %+v", fi)
		}
	}
}
