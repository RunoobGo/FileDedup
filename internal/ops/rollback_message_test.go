package ops

// 回滚失败分支的报错必须指向**真原文件**的位置（2026-09-20 ocr 审查：
// move.go 与 symlink.go 的 step5 在「假链接已挪去 .undo、备份改名回 dup 失败」
// 这一支里把消息写成了 backup+".undo"——那里躺着的是**失败的假链接**，
// 真原文件还在 backup。数据恢复现场把用户指反，等于二次事故）。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/fsid"
)

// installTamperedRename 让改名序列进入最坏分支：
//  1. tmp→dup "成功"但**名不符实**：落位的是逐字节复制的独立文件（身份复核必拦）；
//  2. backup→dup（还原真原文件）**失败**：触发"两份都在、消息指路"的分支。
//
// 返回 backup 路径供断言。
func installTamperedRename(t *testing.T, dup string) string {
	t.Helper()
	orig := hardlinkRename
	tmp := dup + FddTempSuffix
	backup := dup + FddOldSuffix
	hardlinkRename = func(oldpath, newpath string) error {
		switch {
		case oldpath == tmp && newpath == dup:
			b, err := os.ReadFile(oldpath)
			if err != nil {
				return err
			}
			if err := os.WriteFile(newpath, b, 0o644); err != nil {
				return err
			}
			return os.Remove(oldpath)
		case oldpath == backup && newpath == dup:
			return errors.New("模拟：还原备份改名失败")
		}
		return orig(oldpath, newpath)
	}
	t.Cleanup(func() { hardlinkRename = orig })
	return backup
}

// assertRollbackMessagePointsAtRealOriginal 断言错误消息把"原文件保留在"
// 指向 backup 本尊，而不是 .undo 里那份假链接。
func assertRollbackMessagePointsAtRealOriginal(t *testing.T, err error, backup string) {
	t.Helper()
	if err == nil {
		t.Fatal("期望返回错误（还原改名失败必须如实上报）")
	}
	want := fmt.Sprintf("原文件保留在 %s（", backup)
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("错误消息未指向真原文件位置 %q，实际: %v", want, err)
	}
	if strings.Contains(err.Error(), backup+FddOldSuffix+".undo（") ||
		strings.Contains(err.Error(), fmt.Sprintf("原文件保留在 %s.undo", backup)) {
		t.Fatalf("错误消息把假链接的 .undo 说成原文件位置: %v", err)
	}
	// 磁盘现场核对：backup 处必须真的是原独立文件。
	b, rerr := os.ReadFile(backup)
	if rerr != nil {
		t.Fatalf("backup 应仍存在且是真原文件: %v", rerr)
	}
	if string(b) != "ORIGINAL-DUP-DATA" {
		t.Fatalf("backup 内容应为原数据，实际 %q", b)
	}
}

func TestHardlinkMergeRollbackMessageNamesRealOriginal(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep")
	dup := filepath.Join(dir, "dup")
	if err := os.WriteFile(keep, []byte("shared-keep-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dup, []byte("ORIGINAL-DUP-DATA"), 0o644); err != nil {
		t.Fatal(err)
	}
	backup := installTamperedRename(t, dup)

	err := HardlinkMerge(keep, dup, fsid.ID{}, fsid.ID{})
	assertRollbackMessagePointsAtRealOriginal(t, err, backup)
}

func TestSymlinkMergeRollbackMessageNamesRealOriginal(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep")
	dup := filepath.Join(dir, "dup")
	if err := os.WriteFile(keep, []byte("shared-keep-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dup, []byte("ORIGINAL-DUP-DATA"), 0o644); err != nil {
		t.Fatal(err)
	}
	kid, kerr := fsid.FromPath(keep)
	did, derr := fsid.FromPath(dup)
	if kerr != nil || derr != nil {
		t.Skipf("fsid 不可解析，跳过软链接回滚消息测试: %v %v", kerr, derr)
	}
	backup := installTamperedRename(t, dup)

	err := SymlinkMerge(keep, dup, kid, did)
	assertRollbackMessagePointsAtRealOriginal(t, err, backup)
}
