package ops

// §31（2026-09-24 第五轮审查 P0）：XDG 回收站跨卷复制腿「复制完成后按路径
// 盲删源」缺身份复核——AS-H4 在 move.go/undo.go 都已装过的那道守卫，此前
// 唯独没装在这条腿上。判据形状照抄 move_crossvolume_identity_test.go：
// 时序由包级接缝钉死（renameFile 稳定 EXDEV 进入复制腿；copyAndSyncFile 在
// "复制完成、源未删"这一刻构造第三方 rename 顶位；removeSrc 记录是否真动手）。
// 本文件无 build tag：守卫行为在 CI 三腿与开发机同判据真跑（H6 惯例）。

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hijackAfterCopyXDG 与 hijackAfterCopy 同形，差别只在钩的是回收站腿的复制接缝。
func hijackAfterCopyXDG(t *testing.T, srcPath, thirdPartyContent string) *hijackState {
	t.Helper()
	st := &hijackState{}
	origCopy := copyAndSyncFile
	copyAndSyncFile = func(sf io.Reader, dst string, size int64) error {
		if err := origCopy(sf, dst, size); err != nil {
			return err
		}
		st.parked = srcPath + ".third-party-parked"
		if err := os.Rename(srcPath, st.parked); err != nil {
			return err
		}
		return os.WriteFile(srcPath, []byte(thirdPartyContent), 0o644)
	}
	t.Cleanup(func() { copyAndSyncFile = origCopy })

	origRemove := removeSrc
	removeSrc = func(p string) error {
		st.calls++
		return origRemove(p)
	}
	t.Cleanup(func() { removeSrc = origRemove })
	return st
}

func TestTrashXDGCrossVolumeDoesNotDeleteReplacedSource(t *testing.T) {
	root := t.TempDir()
	trashRoot := filepath.Join(root, "trash")
	src := filepath.Join(root, "victim.bin")
	payload := []byte("ORIGINAL-CONTENT-THAT-WAS-COPIED")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	forceCrossVolumeRename(t)
	const thirdParty = "someone else's freshly written file"
	st := hijackAfterCopyXDG(t, src, thirdParty)

	m, err := trashXDG(trashRoot, []string{src})
	if err == nil {
		t.Fatal("复制期间源被第三方顶替，trashXDG 却报告成功：删掉的是第三方文件，账本还会记 done（§31/AS-H4）")
	}
	if !errors.Is(err, errCopiedSrcSwapped) {
		t.Fatalf("错误必须是可分派的哨兵 errCopiedSrcSwapped（trashXDG 靠它决定 trashinfo 不回滚）: %v", err)
	}
	if _, ok := m[src]; ok {
		t.Errorf("被顶替的一腿不是成功，dstMap 不得收该项: %v", m)
	}
	if st.calls != 0 {
		t.Fatalf("守卫触发后不得删任何路径，实际删源调用 %d 次", st.calls)
	}
	got, lerr := os.ReadFile(src)
	if lerr != nil {
		t.Fatalf("第三方文件（现居于被顶替的 src 路径）消失了：它不是本应用的对象，绝不该被动: %v", lerr)
	}
	if string(got) != thirdParty {
		t.Fatalf("第三方文件内容被改动: %q", got)
	}
	if _, lerr := os.Lstat(st.parked); lerr != nil {
		t.Fatalf("被顶替走的原件（%s）消失: %v", st.parked, lerr)
	}
	// 错误文本点名 src 供用户核对（M88 口径），不锁措辞。
	if !strings.Contains(err.Error(), src) {
		t.Errorf("错误文本必须点名 src 路径供用户核对: %v", err)
	}
	// dst 副本 + trashinfo 成对留存（回收站腿与 move 腿的唯一差异，§31.2-3）：
	ents, ferr := os.ReadDir(filepath.Join(trashRoot, "files"))
	if ferr != nil {
		t.Fatal(ferr)
	}
	if len(ents) != 1 {
		t.Fatalf("files/ 应恰好留存一份复制完成的副本，实际 %d 项", len(ents))
	}
	if copied, cerr := os.ReadFile(filepath.Join(trashRoot, "files", ents[0].Name())); cerr != nil {
		t.Errorf("副本读不出: %v", cerr)
	} else if string(copied) != string(payload) {
		t.Errorf("副本内容与顶替前的原件不符: %q", copied)
	}
	infoPath := filepath.Join(trashRoot, "info", ents[0].Name()+".trashinfo")
	if info, ierr := os.ReadFile(infoPath); ierr != nil {
		t.Fatalf("副本必须配 trashinfo（否则成回收站孤儿、用户无从还原，§31.2-3）: %v", ierr)
	} else if !strings.Contains(string(info), "Path=") {
		t.Errorf("trashinfo 缺 Path 行: %q", info)
	}
}

// TestTrashXDGCrossVolumeRemovesUnchangedSource 负控制：复制窗口内源没被动过时，
// 删源必须照常发生（守卫不得过严，否则跨卷回收站退化成复制，占用翻倍）。
func TestTrashXDGCrossVolumeRemovesUnchangedSource(t *testing.T) {
	root := t.TempDir()
	trashRoot := filepath.Join(root, "trash")
	src := filepath.Join(root, "steady.bin")
	if err := os.WriteFile(src, []byte("steady content here"), 0o644); err != nil {
		t.Fatal(err)
	}
	forceCrossVolumeRename(t)

	calls := 0
	origRemove := removeSrc
	removeSrc = func(p string) error {
		calls++
		return origRemove(p)
	}
	t.Cleanup(func() { removeSrc = origRemove })

	m, err := trashXDG(trashRoot, []string{src})
	if err != nil {
		t.Fatalf("未变动的源应正常完成跨卷入站: %v", err)
	}
	if calls != 1 {
		t.Fatalf("删源应恰好调用一次，实际 %d 次", calls)
	}
	if dst, ok := m[src]; !ok || dst == "" {
		t.Fatalf("dstMap 应收下该项: %v", m)
	}
	if _, err := os.Lstat(src); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("入站后源应已删除，实际 Lstat=%v", err)
	}
	ents, ferr := os.ReadDir(filepath.Join(trashRoot, "files"))
	if ferr != nil || len(ents) != 1 {
		t.Fatalf("files/ 应有且只有一份副本: %v (%v)", ents, ferr)
	}
	got, rerr := os.ReadFile(filepath.Join(trashRoot, "files", ents[0].Name()))
	if rerr != nil || string(got) != "steady content here" {
		t.Fatalf("副本内容不符: %q (%v)", got, rerr)
	}
	if _, ierr := os.Lstat(filepath.Join(trashRoot, "info", ents[0].Name()+".trashinfo")); ierr != nil {
		t.Fatalf("正常入站的条目必须带 trashinfo: %v", ierr)
	}
}
