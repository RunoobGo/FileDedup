//go:build !windows

package ops

// H2 回归：VerifyFile 与 HardlinkMerge 之间，keep/dup 路径可能被换成
// 另一个 inode（删旧建新 / rename 覆盖）。合并必须复核身份并在不一致时
// 拦截，且不得动 dup 原文件、不留 .fdd-tmp 残留。

import (
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/fsid"
)

func resolvedID(t *testing.T, p string) fsid.ID {
	t.Helper()
	lst, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	id := fsid.FromFileInfo(lst)
	if !id.Resolved {
		t.Skip("本平台未解析 inode 身份")
	}
	return id
}

func replaceWith(t *testing.T, p, content string) {
	t.Helper()
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHardlinkMergeRejectsReplacedKeep(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep")
	dup := filepath.Join(dir, "dup")
	if err := os.WriteFile(keep, []byte("shared-v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dup, []byte("shared-v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	kid := resolvedID(t, keep)
	did := resolvedID(t, dup)

	replaceWith(t, keep, "attacker-bytes")

	if err := HardlinkMerge(keep, dup, kid, did); err == nil {
		t.Fatal("keep 被替换后合并应被拦截（H2）")
	}
	b, err := os.ReadFile(dup)
	if err != nil || string(b) != "shared-v1" {
		t.Fatalf("拦截后 dup 必须原样保留: %q %v", b, err)
	}
	if _, err := os.Lstat(dup + ".fdd-tmp"); !os.IsNotExist(err) {
		t.Fatalf("拦截后不应残留 .fdd-tmp: %v", err)
	}
}

func TestHardlinkMergeRejectsReplacedDup(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep")
	dup := filepath.Join(dir, "dup")
	if err := os.WriteFile(keep, []byte("shared-v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dup, []byte("shared-v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	kid := resolvedID(t, keep)
	did := resolvedID(t, dup)

	replaceWith(t, dup, "other-content!")

	if err := HardlinkMerge(keep, dup, kid, did); err == nil {
		t.Fatal("dup 被替换后合并应被拦截（H2）")
	}
	b, err := os.ReadFile(dup)
	if err != nil || string(b) != "other-content!" {
		t.Fatalf("拦截后 dup 现状文件不得被动过: %q %v", b, err)
	}
	if _, err := os.Lstat(dup + ".fdd-tmp"); !os.IsNotExist(err) {
		t.Fatalf("拦截后不应残留 .fdd-tmp: %v", err)
	}
	kb, err := os.ReadFile(keep)
	if err != nil || string(kb) != "shared-v1" {
		t.Fatalf("keep 不应被动过: %q %v", kb, err)
	}
}
