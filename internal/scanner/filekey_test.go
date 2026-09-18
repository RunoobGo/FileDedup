package scanner

// FileKey 的语义覆盖（2026-09-19 补）。
//
// 此前 ResolveKey 只有"三平台编译通过"这一级验证，没有任何行为断言，于是
// Windows 侧一处字段接线错误可以无声通过到 CI 才由别的用例间接暴露。
// 阶段 1.5 的硬链接去重整个建立在 FileKey 上，方向必须两头都钉住：
// 不同文件不得同键（漏报整组重复），硬链接必须同键（重复计数与 Reclaimable 虚高）。

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/model"
)

// mkSizedFile 写一个内容为 "payload-<name>" 的文件（名字等长即尺寸等长）。
func mkSizedFile(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("payload-"+name), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// walkEntries 遍历 dir，要求恰好收到 want 条且无失败项。
func walkEntries(t *testing.T, dir string, want int) []*model.FileEntry {
	t.Helper()
	res := Walk(context.Background(), []string{dir}, &model.Filters{}, 2)
	if len(res.Failed) != 0 {
		t.Fatalf("遍历失败清单非空: %+v", res.Failed)
	}
	if len(res.Files) != want {
		t.Fatalf("收文件数 = %d, want %d", len(res.Files), want)
	}
	return res.Files
}

func TestFileKeySeparatesDistinctFiles(t *testing.T) {
	// 名字等长 → 内容等长 → 两条目同 size：只有物理身份能把它们分开。
	root := t.TempDir()
	mkSizedFile(t, root, "a.bin")
	mkSizedFile(t, root, "b.bin")
	files := walkEntries(t, root, 2)
	ka, kb := ResolveKey(files[0]), ResolveKey(files[1])
	if !ka.Resolved || !kb.Resolved {
		t.Skipf("该平台/卷不提供稳定身份，阶段 1.5 退回内容级证据: %+v / %+v", ka, kb)
	}
	if ka == kb {
		t.Fatalf("两个不同文件得到同一 FileKey: %+v（阶段 1.5 会把它们当硬链接合并，整组重复静默消失）", ka)
	}
}

func TestFileKeyMergesHardlinks(t *testing.T) {
	root := t.TempDir()
	src := mkSizedFile(t, root, "a.bin")
	dst := filepath.Join(root, "b.bin")
	if err := os.Link(src, dst); err != nil {
		t.Skipf("该文件系统不支持硬链接: %v", err)
	}
	files := walkEntries(t, root, 2)
	ka, kb := ResolveKey(files[0]), ResolveKey(files[1])
	if !ka.Resolved || !kb.Resolved {
		t.Skipf("该平台/卷不提供稳定身份: %+v / %+v", ka, kb)
	}
	if ka != kb {
		t.Fatalf("同一文件的两个硬链接键不同: %+v vs %+v（重复计数、Reclaimable 虚高）", ka, kb)
	}
}
