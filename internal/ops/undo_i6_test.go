package ops

// 2026-09-18 审查 I6：回撤写前落账留下的崩溃窗口——文件系统已动、账本未收口。
// 重试必须按现状自愈（记成功），而不是永久报「已不存在」把用户逼成死账。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filededup/internal/fsid"
)

// putAt 写文件并把 mtime 精确设为 ns（模拟"上次回撤已搬回并还原时间戳"）。
func putAt(t *testing.T, p string, content []byte, ns int64) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, time.Now(), time.Unix(0, ns)); err != nil {
		t.Fatal(err)
	}
}

// hashOfContent 内容哈希（落到临时文件后复用 hashOfFileT）。
func hashOfContent(t *testing.T, b []byte) [32]byte {
	t.Helper()
	p := filepath.Join(t.TempDir(), "content-src")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return hashOfFileT(t, p)
}

func undoIt(kind, orig, dest string, hash [32]byte, size uint64, ns int64) UndoItem {
	return UndoItem{Kind: kind, OrigPath: orig, DestPath: dest,
		Hash: hash, Size: size, MtimeNs: ns}
}

// 回收站回撤：落点已空、原位证据齐全 → 记成功（返回原位）而非报「不存在」。
func TestUndoTrashSelfHealsAfterCrashBeforeAccounting(t *testing.T) {
	dir := t.TempDir()
	content := []byte("I6-CRASH-RESIDUE-TRASH")
	mtime := pastNs()
	orig := filepath.Join(dir, "home", "a.bin")
	dest := filepath.Join(dir, "trash", "a.bin")
	putAt(t, orig, content, mtime) // 只留原位那份：落点已空 = 已经搬回

	it := undoIt("trash", orig, dest, hashOfContent(t, content), uint64(len(content)), mtime)
	if dst, err := UndoOne(it); err != nil || dst != orig {
		t.Fatalf("崩溃残留应自愈: dst=%q err=%v", dst, err)
	}
	if got, _ := os.ReadFile(orig); string(got) != string(content) {
		t.Fatal("自愈过程改动了已恢复的文件")
	}
}

// 移动回撤同上。
func TestUndoMoveSelfHealsAfterCrashBeforeAccounting(t *testing.T) {
	dir := t.TempDir()
	content := []byte("I6-CRASH-RESIDUE-MOVE")
	mtime := pastNs()
	orig := filepath.Join(dir, "home", "b.bin")
	dest := filepath.Join(dir, "moved", "b.bin")
	putAt(t, orig, content, mtime)

	it := undoIt("move", orig, dest, hashOfContent(t, content), uint64(len(content)), mtime)
	if dst, err := UndoOne(it); err != nil || dst != orig {
		t.Fatalf("移动回撤残留应自愈: dst=%q err=%v", dst, err)
	}
}

// 自愈判定要求落点已空 + 原位尺寸/时间戳/内容全等，缺一即回到正常报错。
func TestAlreadyRestoredRequiresFullEvidence(t *testing.T) {
	dir := t.TempDir()
	content := []byte("EVIDENCE-BASELINE!!!")
	mtime := pastNs()
	hash := hashOfContent(t, content)

	newIt := func(t *testing.T, name string) UndoItem {
		t.Helper()
		orig := filepath.Join(dir, name, "a.bin")
		putAt(t, orig, content, mtime)
		return undoIt("trash", orig, filepath.Join(dir, name, "t", "a.bin"),
			hash, uint64(len(content)), mtime)
	}

	for _, c := range []struct {
		name string
		mut  func(it *UndoItem)
	}{
		{"内容不符", func(it *UndoItem) { it.Hash = [32]byte{0xaa} }},
		{"大小不符", func(it *UndoItem) { it.Size++ }},
		{"时间戳不符", func(it *UndoItem) { it.MtimeNs++ }},
		{"无内容证据", func(it *UndoItem) { it.Hash = [32]byte{} }},
		{"落点未记录", func(it *UndoItem) { it.DestPath = "" }},
	} {
		t.Run(c.name, func(t *testing.T) {
			it := newIt(t, c.name)
			c.mut(&it)
			if alreadyRestored(it) {
				t.Fatal("证据不全却判为已还原")
			}
		})
	}

	t.Run("原位缺失", func(t *testing.T) {
		it := newIt(t, "gone")
		it.OrigPath = filepath.Join(dir, "gone", "still-missing.bin")
		if alreadyRestored(it) {
			t.Fatal("原位没有文件却判为已还原")
		}
	})
	t.Run("落点仍有文件", func(t *testing.T) {
		it := newIt(t, "both")
		putAt(t, it.DestPath, content, mtime) // 两份并存：不是已还原
		if alreadyRestored(it) {
			t.Fatal("落点仍存有文件却判为已还原")
		}
	})
	t.Run("证据齐全", func(t *testing.T) {
		if !alreadyRestored(newIt(t, "full")) {
			t.Fatal("证据齐全却未判为已还原")
		}
	})
}

// 硬链接回撤：拆链已完成（原位独立、内容等于记录）→ 重试记成功而非「inode 不符」。
//
// ★ A2（2026-10-01，docs/05 §0.2 A2／设计段 §6）：这里原来有一格
// `if runtime.GOOS == "windows" { t.Skip("Windows 上 inode 身份未解析，走的是大小降级分支") }`，
// 前提已被两处现读推翻——① fsid I7（句柄身份解析）；② **同文件紧邻的兄弟用例**
// `TestUndoHardlinkReplacedByThirdPartyStillBlocked` 用同一批原语（`HardlinkMerge(keep, dup,
// fsid.ID{}, fsid.ID{})` ＋ 身份复核）却**没有**这格 Skip，而它不在 windows 腿那份
// **完整**名单里（`roster_top=28 ≡ skip_top=28` 自证完整，M404 之后才谈得上）⇒ 按 §6.73
// 判据链＝在 windows 上跑过且绿。Skip 留着就是"用一条作废的前提把一条承重路径永久藏起来"。
// ★ 兑现边界如实：本机是 darwin，删掉 Skip **证不了** windows 会绿；真正的读数在推送后那一跑的
//
//	windows 腿，而按 §6.21 七"本批自己触发的 run 不回写" ⇒ **windows 兑现落在下一批**引用本批日志时。
//	若那一跑它红了，那是 A2 的本意（暴露被 Skip 掩盖的路径），按新缺陷取号，不当夹具问题抹掉。
func TestUndoHardlinkSelfHealsAfterUnlinkCrash(t *testing.T) {
	dir := t.TempDir()
	content := []byte("I6-HARDLINK-RESIDUE!!!")
	keep := filepath.Join(dir, "keep.bin")
	dup := filepath.Join(dir, "dup.bin")
	putAt(t, keep, content, pastNs())
	putAt(t, dup, content, pastNs())
	if err := HardlinkMerge(keep, dup, fsid.ID{}, fsid.ID{}); err != nil {
		t.Fatal(err)
	}
	it := undoIt("hardlink", dup, "", hashOfContent(t, content), uint64(len(content)), pastNs())
	it.LinkSrc = keep
	if _, err := UndoOne(it); err != nil {
		t.Fatal(err)
	}
	if os.SameFile(statT(t, keep), statT(t, dup)) {
		t.Fatal("首次回撤未拆链")
	}
	// 第二次：账本没落上（写前标记残留）→ 必须自愈为成功
	dst, err := UndoOne(it)
	if err != nil {
		t.Fatalf("已拆链的残留应自愈而非拦成死账: %v", err)
	}
	if dst != dup {
		t.Fatalf("自愈返回路径不符: %s", dst)
	}
}

// 原位被换成第三方内容 → 仍然拦截（自愈不得放水）。
func TestUndoHardlinkReplacedByThirdPartyStillBlocked(t *testing.T) {
	dir := t.TempDir()
	content := []byte("I6-HARDLINK-BLOCK-BASE")
	keep := filepath.Join(dir, "keep.bin")
	dup := filepath.Join(dir, "dup.bin")
	putAt(t, keep, content, pastNs())
	putAt(t, dup, content, pastNs())
	if err := HardlinkMerge(keep, dup, fsid.ID{}, fsid.ID{}); err != nil {
		t.Fatal(err)
	}
	other := []byte("someone-elses-copy-x")
	putAt(t, dup, other, pastNs()) // 独立文件、内容不同
	it := undoIt("hardlink", dup, "", hashOfContent(t, content), uint64(len(content)), pastNs())
	it.LinkSrc = keep
	if _, err := UndoOne(it); err == nil || !strings.Contains(err.Error(), "已拦截") {
		t.Fatalf("换成第三方内容必须拦截: %v", err)
	}
	if got, _ := os.ReadFile(dup); string(got) != string(other) {
		t.Fatal("拦截后改动了第三方文件")
	}
}
