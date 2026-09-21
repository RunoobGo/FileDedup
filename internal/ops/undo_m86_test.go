package ops

// M86（04 §6.11 OPS-14b；设计段 §19.0-2 / §19.1-6）的"修前必红"探针。
//
// UndoOne 有三条"部分成功"路径：数据已经落回磁盘了，但收尾动作失败，盘上留下两份。
// 这三条都返回 (target, err) 双值 —— 用户此刻最怕的就是"不知道哪份是真的"，
// 所以错误文本必须自己说清落点在哪。
//
// 取证结论（§19.0-2）：登记原文写的是"落点必然丢失"，实测要收窄 ——
// app.go 的 undoExecuteItem 确实把 restored 丢成 ""，但另两条路径的落点本来就写在
// 文案里、走 FailedItem.Err 送达用户，只有 undo.go:178（已复制但清理回收站侧失败）
// 这一条的文案里根本没有路径。所以本用例判据定在**错误文本**上（用户可见的那条通道），
// 三条各自钉一格：改前 A/C 绿、B 红；B 那一格就是 M86 的真缺口。
//
// 三条都走已有的包内接缝（renameFile / copyVerifyFile / removeSrc / hardlinkRename），
// 全是确定性场景，没有 sleep 赌时序。

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// undoPartial 一条"部分成功"路径的运行结果：err 与"数据现在在哪"。
// wantPath 由构造器自己算出来，不用例子里再猜一遍——它必须出现在错误文本里。
type undoPartial struct {
	dst      string
	wantPath string
	err      error
}

// A：跨卷恢复后回收站侧被第三方顶替 ⇒ 未清理回收站侧（undo.go:174）。
func partialDestReplacedDuringCopy(t *testing.T) undoPartial {
	t.Helper()
	dir := t.TempDir()
	rbin := filepath.Join(dir, "rbin")
	orig := filepath.Join(dir, "orig.bin")
	if err := os.MkdirAll(rbin, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(rbin, "victim.bin")
	payload := []byte("M86-A-TRASHED-PAYLOAD")
	if err := os.WriteFile(dest, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	forceCrossVolumeRename(t)
	hijackAfterCopy(t, dest, "third party wrote here")

	it := UndoItem{Kind: "trash", OrigPath: orig, DestPath: dest,
		Hash: hashOfContent(t, payload), Size: uint64(len(payload))}
	dst, err := UndoOne(it)
	return undoPartial{dst: dst, wantPath: orig, err: err}
}

// B：复制已到家、回收站侧删不掉 ⇒ 两份并存（undo.go:178）。M86 的真缺口。
func partialSrcCleanupFailed(t *testing.T) undoPartial {
	t.Helper()
	dir := t.TempDir()
	rbin := filepath.Join(dir, "rbin")
	orig := filepath.Join(dir, "orig.bin")
	if err := os.MkdirAll(rbin, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(rbin, "victim.bin")
	payload := []byte("M86-B-TRASHED-PAYLOAD")
	if err := os.WriteFile(dest, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	forceCrossVolumeRename(t)
	origRemove := removeSrc
	removeSrc = func(string) error { return errors.New("模拟：回收站侧句柄被占用，删不掉") }
	t.Cleanup(func() { removeSrc = origRemove })

	it := UndoItem{Kind: "trash", OrigPath: orig, DestPath: dest,
		Hash: hashOfContent(t, payload), Size: uint64(len(payload))}
	dst, err := UndoOne(it)
	// 前提自检（三条都跑在前面，免得断言落在一个没成立的状态上）：
	if err == nil {
		t.Fatal("前提自检：删源失败时 UndoOne 必须报错")
	}
	if b, rerr := os.ReadFile(orig); rerr != nil || string(b) != string(payload) {
		t.Fatalf("前提自检：数据应已复制到原位 %s（rerr=%v got=%q）——落点这个词必须是真实存在的路径", orig, rerr, b)
	}
	if _, lerr := os.Lstat(dest); lerr != nil {
		t.Fatalf("前提自检：回收站侧应仍存在（删失败才有两份并存）: %v", lerr)
	}
	// 结构腿：target 改前就返回出来了（app 层随后丢掉，另见 P-19-7b）。
	if dst != orig {
		t.Fatalf("前提自检：target 应为 %s，实测 %q", orig, dst)
	}
	return undoPartial{dst: dst, wantPath: orig, err: err}
}

// C：软链接回撤，链接已删但还原备份改名失败 ⇒ 数据在 backup（undo.go:397）。
func partialSymlinkRestoreFailed(t *testing.T) undoPartial {
	t.Helper()
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.bin")
	payload := []byte("M86-C-KEPT-SOURCE")
	if err := os.WriteFile(keep, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	orig := filepath.Join(dir, "dup.bin")
	backup := orig + FddOldSuffix
	if err := os.WriteFile(backup, []byte("the pre-merge independent copy"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(keep, orig); err != nil {
		t.Fatal(err)
	}
	origRename := hardlinkRename
	hardlinkRename = func(_, _ string) error { return errors.New("模拟：还原备份时改名失败") }
	t.Cleanup(func() { hardlinkRename = origRename })

	it := UndoItem{Kind: "symlink", OrigPath: orig, LinkSrc: keep,
		Hash: hashOfContent(t, payload), Size: uint64(len(payload))}
	dst, err := UndoOne(it)
	if err == nil {
		t.Fatal("前提自检：还原改名失败时 UndoOne 必须报错")
	}
	return undoPartial{dst: dst, wantPath: backup, err: err}
}

// TestUndoPartialRestoreMessagesNameTheLandingPath P-19-7（表驱动三条）。
//
// 判据取"错误文本含该条的落点路径"而不是"含双值里的 target"：后者改前就成立，
// 钉的是内部返回值；用户能看到的只有 Err 与账本 err 列（FailedDrawer 原样渲染），
// 所以只有文本含路径才算送达（§19.0-2 的可用性取证）。
func TestUndoPartialRestoreMessagesNameTheLandingPath(t *testing.T) {
	cases := []struct {
		name string
		run  func(t *testing.T) undoPartial
	}{
		{"回收站侧被顶替-未清理", partialDestReplacedDuringCopy},
		{"已复制但清理回收站侧失败", partialSrcCleanupFailed},
		{"链接已删-还原备份失败", partialSymlinkRestoreFailed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := c.run(t)
			if p.err == nil {
				t.Fatal("部分成功路径必须报错（盘上有两份，不能报成功）")
			}
			if !strings.Contains(p.err.Error(), p.wantPath) {
				t.Errorf("错误文本没说出落点 %s，用户拿到的是一句「两份并存」却不知道去哪找：%q",
					p.wantPath, p.err.Error())
			}
		})
	}
}
