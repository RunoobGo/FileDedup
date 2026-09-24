package scanner

// B5（R-扫描-1，2026-09-24 第五轮审查 P0 批）：DT_UNKNOWN 目录静默漏扫。
//
// os.ReadDir 交出的 DirEntry，其 Type() 在 FUSE/SMB/部分网络挂载上可能返回 0
// （d_type 未回填，即 DT_UNKNOWN）。而 Type() 对**常规文件**同样返回 0
// （FileMode.Type() 只保留类型位，常规文件无任何类型位）——派发前无法区分二者。
// 于是 DT_UNKNOWN 的目录会错走文件腿，被 `!info.Mode().IsRegular() → continue`
// 静默丢弃：整棵子树漏扫、不记 Failed、不计数，违背 M21「跳过必留痕」纪律。
//
// 本文件无 build tag：兜底分支在 CI 三腿与开发机同判据真跑（H6 惯例）。真实卷都填
// d_type，造不出 DT_UNKNOWN，故由 readDirFn 接缝注入（同 guard/cloudCheck 手法）。

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// unknownTypeEntry 把一个真实 DirEntry 包装成「d_type 未知」：Type() 恒返回 0，
// Info() 仍委托真实项（目录则 info.IsDir() 为真）。这正是 DT_UNKNOWN 的形状——
// 派发前看不出是目录，但 Info() 一次 stat 就能报出真实 mode。
type unknownTypeEntry struct{ fs.DirEntry }

func (unknownTypeEntry) Type() fs.FileMode { return 0 }

// TestScannerDirUnknownTypeNotSilentlySkipped：DT_UNKNOWN 的目录项不得被文件腿静默
// 丢弃——整棵子树必须照常遍历，且不记 Failed（它不是失败，是本就该 traversed 的目录）。
func TestScannerDirUnknownTypeNotSilentlySkipped(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	mkDirFiles(t, sub, "inner.txt") // root/sub/inner.txt

	orig := readDirFn
	t.Cleanup(func() { readDirFn = orig })
	readDirFn = func(name string) ([]fs.DirEntry, error) {
		ents, err := os.ReadDir(name)
		if err != nil {
			return nil, err
		}
		// 只在读 root 时把 "sub" 伪装成 DT_UNKNOWN；sub 内部照常返回真实项，
		// 让遍历被提交后能正常下潜到 inner.txt。
		if filepath.Clean(name) != filepath.Clean(root) {
			return ents, nil
		}
		out := make([]fs.DirEntry, len(ents))
		for i, e := range ents {
			if e.Name() == "sub" {
				out[i] = unknownTypeEntry{e}
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
		t.Fatalf("DT_UNKNOWN 目录 sub 的子树被静默漏扫：files=%v，want 含 %s"+
			"（修前 sub 错走文件腿、被 !IsRegular 丢弃，整棵子树无声消失）",
			filePaths(res.Files), want)
	}
	// 漏扫是静默的（不记 Failed）；修后既遍历到，也不该凭空给 sub 记一条失败。
	for _, fi := range res.Failed {
		if filepath.Clean(fi.Path) == filepath.Clean(sub) {
			t.Errorf("sub 不应记 Failed（DT_UNKNOWN 目录应被正常遍历，不是失败）: %+v", fi)
		}
	}
}
