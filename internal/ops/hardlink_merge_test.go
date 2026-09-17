package ops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestHardlinkMerge_Success 验证正常合并：dup 成为指向 keep 的硬链接，
// 内容一致，且无 .fdd-tmp / .fdd-old 残留。
func TestHardlinkMerge_Success(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep")
	dup := filepath.Join(dir, "dup")
	if err := os.WriteFile(keep, []byte("shared"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dup, []byte("shared"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := HardlinkMerge(keep, dup); err != nil {
		t.Fatalf("HardlinkMerge 失败: %v", err)
	}
	ki, _ := os.Stat(keep)
	di, _ := os.Stat(dup)
	if !os.SameFile(ki, di) {
		t.Fatalf("dup 应成为 keep 的硬链接（同 inode）")
	}
	b, err := os.ReadFile(dup)
	if err != nil || string(b) != "shared" {
		t.Fatalf("dup 内容应一致: %q %v", b, err)
	}
	if _, err := os.Stat(dup + ".fdd-old"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("合并成功后不应残留 backup")
	}
	if _, err := os.Stat(dup + ".fdd-tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("合并成功后不应残留 tmp")
	}
}

// TestHardlinkMerge_RollbackOnRenameFailure 模拟「删除 dup 成功后、把临时硬链接
// 改名回 dup 失败」这一极罕见路径：必须保证 dup 路径被恢复、原内容不丢，
// 且 keep 不受影响。修复前此处会 os.Remove(tmp) 导致 dup 永久丢失。
func TestHardlinkMerge_RollbackOnRenameFailure(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep")
	dup := filepath.Join(dir, "dup")
	if err := os.WriteFile(keep, []byte("keepdata"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dup, []byte("dupdata"), 0o644); err != nil {
		t.Fatal(err)
	}

	orig := hardlinkRename
	defer func() { hardlinkRename = orig }()
	// 无条件让重命名失败，复现「删除 dup 成功后改名回 dup 失败」的极罕见路径。
	hardlinkRename = func(_, _ string) error {
		return errors.New("simulated rename failure")
	}

	if err := HardlinkMerge(keep, dup); err == nil {
		t.Fatal("期望 Rename 失败时返回错误")
	}
	di, err := os.Stat(dup)
	if err != nil {
		t.Fatalf("dup 应已恢复存在（修复前会丢失）: %v", err)
	}
	b, err := os.ReadFile(dup)
	if err != nil || string(b) != "dupdata" {
		t.Fatalf("dup 内容应保留原值: %q %v", b, err)
	}
	_ = di
	kb, err := os.ReadFile(keep)
	if err != nil || string(kb) != "keepdata" {
		t.Fatalf("keep 内容应不变: %v", err)
	}
}
