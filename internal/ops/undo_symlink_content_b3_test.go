package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// B3（R-操作-1）：undoSymlink 的防线② 此前只查备份**存在性**（os.Lstat），不查内容，
// 与 undoHardlink 防线②（copy 后全量 BLAKE3 对 it.Hash）不对称。`.fdd-old` 名被扫描器
// 按 worktemp.IsTempName 忽略，第三方顶替备份对用户不可见；若只查存在就删链接 + 改名，
// 会把顶替者当原文件还原回 OrigPath 并 applyMtime 拨回记录时间——用户原数据已被顶替覆盖，
// 眼下又多一个冒名文件。本测试钉住：还原前对备份做全量 BLAKE3 复核，不符即拦截、链接与
// 备份均未动。
func TestUndoSymlinkBackupContentRecheck(t *testing.T) {
	dir := t.TempDir()
	requireSymlinkSupport(t, dir)
	keep := filepath.Join(dir, "keep.bin")
	if err := os.WriteFile(keep, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	dup := filepath.Join(dir, "dup.bin")
	if err := os.Symlink(keep, dup); err != nil { // ① 合法链接（指向仍存在的 keep）
		t.Fatal(err)
	}
	backup := dup + FddOldSuffix
	trueOriginal := []byte("true-original-content")
	if err := os.WriteFile(backup, trueOriginal, 0o644); err != nil {
		t.Fatal(err)
	}
	// 记录哈希 = 真原文件内容的 BLAKE3（合并时写进账本的那份证据）。
	h, err := hashFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	// 第三方顶替备份：.fdd-old 扫描忽略，顶替不可见。
	swapped := []byte("THIRD-PARTY-SWAPPED")
	if err := os.WriteFile(backup, swapped, 0o644); err != nil {
		t.Fatal(err)
	}

	it := UndoItem{
		Kind: "symlink", OrigPath: dup, LinkSrc: keep,
		Hash: h, Size: uint64(len(trueOriginal)),
	}

	_, err = undoSymlink(it)
	if err == nil {
		t.Fatal("备份内容与记录哈希不符时必须拦截，否则会把顶替者当原文件还原")
	}
	if !strings.Contains(err.Error(), "不符") && !strings.Contains(err.Error(), "顶替") {
		t.Fatalf("应报「备份内容不符 / 疑似被顶替」，got %v", err)
	}
	// 拦截即不得动任何文件：链接仍是链接，备份仍是顶替者那份（未被改名/删除）。
	li, lerr := os.Lstat(dup)
	if lerr != nil || li.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("软链接必须原封不动：err=%v", lerr)
	}
	got, rerr := os.ReadFile(backup)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(got) != string(swapped) {
		t.Fatalf("备份不得被改动（拦截即不碰任何文件）：got %q", got)
	}
}

// 反向钉住 fail-open 口径：it.Hash 为零值（升级前旧账本无内容证据）时**跳过**复核而非
// 拦死，照常还原——与 undoHardlink 防线②、undoSourceCheck、alreadyRestored 同判据。
// 防止后人把复核写成"零值哈希也拦"，那会把所有旧账本的软链接回撤永久堵死。
func TestUndoSymlinkBackupZeroHashLegacyRestores(t *testing.T) {
	dir := t.TempDir()
	requireSymlinkSupport(t, dir)
	keep := filepath.Join(dir, "keep.bin")
	if err := os.WriteFile(keep, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	dup := filepath.Join(dir, "dup.bin")
	if err := os.Symlink(keep, dup); err != nil {
		t.Fatal(err)
	}
	backup := dup + FddOldSuffix
	origBytes := []byte("legacy-original")
	if err := os.WriteFile(backup, origBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	it := UndoItem{
		Kind: "symlink", OrigPath: dup, LinkSrc: keep,
		Hash: [32]byte{}, // 零值：旧账本无内容证据
		Size: uint64(len(origBytes)),
	}

	restored, err := undoSymlink(it)
	if err != nil {
		t.Fatalf("零值哈希应按旧账本口径放行还原，却报错：%v", err)
	}
	if restored != dup {
		t.Fatalf("restored = %q, want %q", restored, dup)
	}
	got, rerr := os.ReadFile(dup)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(got) != string(origBytes) {
		t.Fatalf("还原后 OrigPath 内容 = %q, want 备份原内容 %q", got, origBytes)
	}
	if _, e := os.Lstat(backup); !os.IsNotExist(e) {
		t.Fatalf("备份应已被改名回原位（不再独立存在），got err=%v", e)
	}
}
