package ops

// Task 10：回撤原语端到端测试（temp dir，覆盖 spec §7.2 三类可撤操作与拒绝路径）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filededup/internal/fsid"
	"filededup/internal/hasher"
)

func hashOfFileT(t *testing.T, p string) [32]byte {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	pool := hasher.NewPool()
	buf := pool.GetStreamBuf()
	defer pool.PutStreamBuf(buf)
	h, err := hasher.HashFull(f, st.Size(), buf)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func statT(t *testing.T, p string) os.FileInfo {
	t.Helper()
	st, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat %s: %v", p, err)
	}
	return st
}

// 共同基线：过去时刻的 mtime，回撤后必须精确还原。
func pastNs() int64 { return time.Now().Add(-72 * time.Hour).UnixNano() }

func TestUndoTrashRestoresToOrig(t *testing.T) {
	dir := t.TempDir()
	content := []byte("TRASH-UNDO-PAYLOAD-内容")
	dest := filepath.Join(dir, ".Trash", "files", "a.bin")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, content, 0o644); err != nil {
		t.Fatal(err)
	}
	orig := filepath.Join(dir, "orig", "a.bin")
	mtime := pastNs()
	it := UndoItem{Kind: "trash", OrigPath: orig, DestPath: dest,
		Hash: hashOfFileT(t, dest), Size: uint64(len(content)), MtimeNs: mtime}

	dst, err := UndoOne(it)
	if err != nil {
		t.Fatal(err)
	}
	if dst != orig {
		t.Fatalf("回原位失败: %s", dst)
	}
	got, err := os.ReadFile(orig)
	if err != nil || string(got) != string(content) {
		t.Fatalf("内容不符: %q err=%v", got, err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("回收站侧残留")
	}
	if st := statT(t, orig); st.ModTime().UnixNano() != mtime {
		t.Fatalf("mtime 未还原: %v want %v", st.ModTime(), time.Unix(0, mtime))
	}
}

func TestUndoTrashOrigOccupiedUsesRestoredName(t *testing.T) {
	dir := t.TempDir()
	content := []byte("OCCUPIED-CASE")
	dest := filepath.Join(dir, "trashbin", "a.bin")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, content, 0o644); err != nil {
		t.Fatal(err)
	}
	orig := filepath.Join(dir, "a.bin")
	occupant := []byte("someone-else")
	if err := os.WriteFile(orig, occupant, 0o644); err != nil {
		t.Fatal(err)
	}
	it := UndoItem{Kind: "trash", OrigPath: orig, DestPath: dest,
		Hash: hashOfFileT(t, dest), Size: uint64(len(content)), MtimeNs: pastNs()}

	dst, err := UndoOne(it)
	if err != nil {
		t.Fatal(err)
	}
	if dst != filepath.Join(dir, "a.fdd-restored.bin") {
		t.Fatalf("落地名不符: %s", dst)
	}
	if got, _ := os.ReadFile(orig); string(got) != string(occupant) {
		t.Fatal("占位文件被覆盖")
	}
	if got, _ := os.ReadFile(dst); string(got) != string(content) {
		t.Fatal("恢复内容不符")
	}
}

// TestUndoTrashSecondOccupiedLandsAsWorkTemp AS-R1（2026-09-20 全仓审计）端到端：
// 原位与 a.fdd-restored.bin 都被占时，第二次恢复经 claimDst 落到
// a.fdd-restored_1.bin —— 序号插在扩展名**之前**，把标记与扩展名隔开了。
// 这个名字必须仍被 worktemp 认出来，否则恢复产物会重新参与重复分组，
// 用户看到的还是缺陷 6 那句「刚恢复的文件重扫又变重复」。
func TestUndoTrashSecondOccupiedLandsAsWorkTemp(t *testing.T) {
	dir := t.TempDir()
	content := []byte("SECOND-OCCUPIED-CASE")
	dest := filepath.Join(dir, "trashbin", "a.bin")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, content, 0o644); err != nil {
		t.Fatal(err)
	}
	orig := filepath.Join(dir, "a.bin")
	if err := os.WriteFile(orig, []byte("someone-else"), 0o644); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(dir, "a.fdd-restored.bin")
	if err := os.WriteFile(first, []byte("first-restore"), 0o644); err != nil {
		t.Fatal(err)
	}

	it := UndoItem{Kind: "trash", OrigPath: orig, DestPath: dest,
		Hash: hashOfFileT(t, dest), Size: uint64(len(content)), MtimeNs: pastNs()}
	dst, err := UndoOne(it)
	if err != nil {
		t.Fatal(err)
	}
	if dst != filepath.Join(dir, "a.fdd-restored_1.bin") {
		t.Fatalf("第二次恢复应落到带序号的名字，实得 %s", dst)
	}
	if !IsWorkTempName(filepath.Base(dst)) {
		t.Fatalf("%q 是恢复产物，必须判为工作临时名（否则残留会重新参与重复分组）", filepath.Base(dst))
	}
	// 两份既有文件都不许被动过
	if got, _ := os.ReadFile(orig); string(got) != "someone-else" {
		t.Fatal("原位占位文件被覆盖")
	}
	if got, _ := os.ReadFile(first); string(got) != "first-restore" {
		t.Fatal("第一次恢复的产物被覆盖")
	}
}

func TestUndoTrashDestMissing(t *testing.T) {
	dir := t.TempDir()
	it := UndoItem{Kind: "trash", OrigPath: filepath.Join(dir, "gone", "a.bin"),
		DestPath: filepath.Join(dir, "empty", "a.bin"), Size: 5, MtimeNs: pastNs()}
	if _, err := UndoOne(it); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("dest 缺失应报错: %v", err)
	}
}

func TestUndoTrashDestSizeMismatch(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "t", "a.bin")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("different-length!!"), 0o644); err != nil {
		t.Fatal(err)
	}
	it := UndoItem{Kind: "trash", OrigPath: filepath.Join(dir, "a.bin"),
		DestPath: dest, Size: 3, MtimeNs: pastNs()}
	if _, err := UndoOne(it); err == nil || !strings.Contains(err.Error(), "大小") {
		t.Fatalf("size 与记录不符应报错: %v", err)
	}
}

func TestUndoMoveReturnsToOrigDir(t *testing.T) {
	dir := t.TempDir()
	content := []byte("MOVE-UNDO-BYTES")
	dest := filepath.Join(dir, "moved", "b.bin")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, content, 0o644); err != nil {
		t.Fatal(err)
	}
	orig := filepath.Join(dir, "home", "b.bin")
	mtime := pastNs()
	it := UndoItem{Kind: "move", OrigPath: orig, DestPath: dest,
		Hash: hashOfFileT(t, dest), Size: uint64(len(content)), MtimeNs: mtime}

	dst, err := UndoOne(it)
	if err != nil {
		t.Fatal(err)
	}
	if dst != orig {
		t.Fatalf("回迁位置不符: %s", dst)
	}
	if got, _ := os.ReadFile(orig); string(got) != string(content) {
		t.Fatal("回迁内容不符")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("原移动目标残留")
	}
	if st := statT(t, orig); st.ModTime().UnixNano() != mtime {
		t.Fatalf("mtime 未还原: %v", st.ModTime())
	}
}

// 合并后拆链：dup 恢复为独立文件，内容仍等于组哈希，keep 不受影响。
func TestUndoHardlinkUnlinksAndKeepsContent(t *testing.T) {
	dir := t.TempDir()
	content := []byte("HARDLINK-UNDO-CONTENT!!")
	keep := filepath.Join(dir, "keep.bin")
	dup := filepath.Join(dir, "sub", "dup.bin")
	if err := os.MkdirAll(filepath.Dir(dup), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keep, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dup, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := HardlinkMerge(keep, dup, fsid.ID{}, fsid.ID{}); err != nil {
		t.Fatal(err)
	}
	mtime := pastNs()
	it := UndoItem{Kind: "hardlink", OrigPath: dup, LinkSrc: keep,
		Hash: hashOfFileT(t, keep), Size: uint64(len(content)), MtimeNs: mtime}

	dst, err := UndoOne(it)
	if err != nil {
		t.Fatal(err)
	}
	if dst != dup {
		t.Fatalf("恢复路径不符: %s", dst)
	}
	ki, di := statT(t, keep), statT(t, dup)
	if os.SameFile(ki, di) {
		t.Fatal("dup 仍与 keep 共享 inode，未拆链")
	}
	if hashOfFileT(t, dup) != it.Hash {
		t.Fatal("dup 内容与组哈希不符")
	}
	if got, _ := os.ReadFile(keep); string(got) != string(content) {
		t.Fatal("keep 被改动")
	}
	if di.ModTime().UnixNano() != mtime {
		t.Fatalf("mtime 未还原: %v", di.ModTime())
	}
}

// 保留源被篡改（内容 ≠ 记录哈希）→ 拦截，dup 现状不动。
func TestUndoHardlinkSrcTampered(t *testing.T) {
	dir := t.TempDir()
	content := []byte("ORIGINAL-CONTENT-AAAA")
	keep := filepath.Join(dir, "keep.bin")
	dup := filepath.Join(dir, "dup.bin")
	if err := os.WriteFile(keep, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dup, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := HardlinkMerge(keep, dup, fsid.ID{}, fsid.ID{}); err != nil {
		t.Fatal(err)
	}
	origHash := hashOfFileT(t, keep)
	// 原地改写 keep（同 inode 场景下 dup 内容同步变化，模拟源被篡改）
	if err := os.WriteFile(keep, []byte("TAMPERED-BY-SOMEONE-ELSE"), 0o644); err != nil {
		t.Fatal(err)
	}
	it := UndoItem{Kind: "hardlink", OrigPath: dup, LinkSrc: keep,
		Hash: origHash, Size: uint64(len(content)), MtimeNs: pastNs()}

	before := statT(t, dup)
	if _, err := UndoOne(it); err == nil || !strings.Contains(err.Error(), "不一致") {
		t.Fatalf("源被篡改应拦截: %v", err)
	}
	after := statT(t, dup)
	if !os.SameFile(before, after) || after.Size() != before.Size() {
		t.Fatal("dup 现状被改动")
	}
	tmpLeft, _ := filepath.Glob(dup + ".fdd-*")
	if len(tmpLeft) > 0 {
		t.Fatalf("临时文件残留: %v", tmpLeft)
	}
}

// 用户已删除 dup 路径 → 报「目标已不存在」，不误建文件。
func TestUndoHardlinkTargetMissing(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.bin")
	if err := os.WriteFile(keep, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	it := UndoItem{Kind: "hardlink", OrigPath: filepath.Join(dir, "gone.bin"),
		LinkSrc: keep, Size: 1}
	if _, err := UndoOne(it); err == nil || !strings.Contains(err.Error(), "目标已不存在") {
		t.Fatalf("dup 已删除应报错: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gone.bin")); !os.IsNotExist(err) {
		t.Fatal("不应凭空创建恢复文件")
	}
}

func TestUndoDeleteRejected(t *testing.T) {
	_, err := UndoOne(UndoItem{Kind: "delete", OrigPath: "/nope"})
	if err == nil || !strings.Contains(err.Error(), "永久删除不可回撤") {
		t.Fatalf("delete 应明确拒绝: %v", err)
	}
}

func TestUndoUnknownKind(t *testing.T) {
	if _, err := UndoOne(UndoItem{Kind: "purge"}); err == nil {
		t.Fatal("未知类型应报错")
	}
}
