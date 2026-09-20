package ops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// undoSymlink 的「悬空链接」分支（顺带项，2026-09-20 全仓审计）：
// 该分支要把两种**性质完全不同**的情形分开——
//
//	A. 链接的字面目标不是记录的保留源 → 真拦截（第三方改动过）
//	B. readlink 自己失败（I/O 错误、平台不支持）→ 现状未知，不是"不符"
//
// 修正前 A 与 B 合并成同一句「不指向原保留源」，且绕过包内跨平台访问器
// symlinkTarget 直接调 os.Readlink。B 被说成 A 时，用户看到的是一份
// 并不存在的"第三方链接"，而真实原因（比如磁盘错误）被吞掉。

// danglingLinkFixture 造出「保留源已消失、原位仍是指向它的软链接」+ 备份，
// 使 undoSymlink 必然进入 verifySymlinked 失败且 Stat(LinkSrc) 失败的分支。
func danglingLinkFixture(t *testing.T) (it UndoItem, backup string) {
	t.Helper()
	dir := t.TempDir()
	requireSymlinkSupport(t, dir)          // 不支持建链接的环境（未提权 Windows）早跳过
	keep := filepath.Join(dir, "keep.bin") // 建链后即删除 → 悬空
	if err := os.WriteFile(keep, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	dup := filepath.Join(dir, "dup.bin")
	if err := os.Symlink(keep, dup); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(keep); err != nil {
		t.Fatal(err)
	}
	backup = dup + FddOldSuffix
	if err := os.WriteFile(backup, []byte("original-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	return UndoItem{Kind: "symlink", OrigPath: dup, LinkSrc: keep}, backup
}

// TestUndoSymlinkReportsReadlinkFailureAsIOError B 类：读链接失败必须
// 原样包出 I/O 原因，不得谎报"链接不指向原保留源"。
func TestUndoSymlinkReportsReadlinkFailureAsIOError(t *testing.T) {
	it, backup := danglingLinkFixture(t)

	orig := readlinkTarget
	defer func() { readlinkTarget = orig }()
	wantErr := errors.New("simulated EIO on readlink")
	readlinkTarget = func(string) (string, error) { return "", wantErr }

	_, err := undoSymlink(it)
	if err == nil {
		t.Fatal("读链接失败时不得放行回撤（现状未知就动手删链接）")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("失败原因必须用 %%w 包出底层 I/O 错误，got %v", err)
	}
	if strings.Contains(err.Error(), "不指向原保留源") {
		t.Fatalf("I/O 失败被误诊为「链接指向别处」（用户会去找根本不存在的第三方链接）: %v", err)
	}
	if _, e := os.Lstat(backup); e != nil {
		t.Fatal("备份必须完好无损（拦截即不得动任何文件）")
	}
}

// TestUndoSymlinkStillInterceptsForeignTarget 负例（防过松）：真正的
// "指向别处"必须照旧拦截，并且把实际指向报出来。加了接缝之后这条最容易被
// 顺手改松——所以它和上一条成对存在。
func TestUndoSymlinkStillInterceptsForeignTarget(t *testing.T) {
	dir := t.TempDir()
	it, backup := danglingLinkFixture(t)
	foreign := filepath.Join(dir, "somewhere-else.bin")
	if err := os.Remove(it.OrigPath); err != nil { // 换掉用户自己建的链接
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, it.OrigPath); err != nil {
		t.Fatal(err)
	}

	_, err := undoSymlink(it)
	if err == nil {
		t.Fatal("链接指向别处必须拦截，否则会删掉用户的第三方链接")
	}
	if !strings.Contains(err.Error(), "不指向原保留源") {
		t.Fatalf("应报「不指向原保留源」，got %v", err)
	}
	if strings.Contains(err.Error(), "读取软链接目标失败") {
		t.Fatalf("这不是 I/O 失败，不该借用 I/O 文案: %v", err)
	}
	if _, e := os.Lstat(it.OrigPath); e != nil {
		t.Fatal("用户的链接必须原封不动")
	}
	if _, e := os.Lstat(backup); e != nil {
		t.Fatal("备份必须原封不动")
	}
}
