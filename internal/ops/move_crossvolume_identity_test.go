package ops

// AS-H4（2026-09-20 全仓审计，第 7 篇 §三）：跨卷「复制 + 删源」在删源前没有身份复核。
//
// 复制（copyVerify）可持续数秒到数分钟，是全部已加固点里**窗口最大**的一个，
// 而删源用的是纯路径操作 os.Remove(src)。窗口内第三方以 rename 顶替该路径
// （同步盘、下载器的原子写入正是这个时序）→ 应用删掉别人的新文件，账本还记 done。
//
// 修法：复制前经已开句柄取身份，删源前 identityStill 复核；不符则保留源、
// 报错并计失败（宁可留两份让人核对，也不替用户删掉他没打算删的文件）。
//
// 时序由两个包级接缝钉死（同 hardlinkRename 的既有惯例）：
//   - renameFile：让首次 rename 稳定返回 EXDEV，确定性地进入跨卷分支；
//   - copyVerifyFile：在「复制已完成、源尚未删除」这一刻完成第三方顶替；
//   - removeSrc：记录删源是否真的发生（守卫生效时必须一次都没调用）。
// 修正前没有删源前复核，removeSrc 就是裸 os.Remove → 第三方文件被删 → 红。

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// forceCrossVolumeRename 让 MoveFile / undoTrash 的首次 rename 稳定失败于 EXDEV。
func forceCrossVolumeRename(t *testing.T) {
	t.Helper()
	orig := renameFile
	renameFile = func(oldPath, newPath string) error {
		return &os.LinkError{Op: "rename", Old: oldPath, New: newPath, Err: syscall.EXDEV}
	}
	t.Cleanup(func() { renameFile = orig })
}

// hijackState 承接钩子内才确定的信息（闭包赋值晚于函数返回，故必须用指针字段）。
type hijackState struct {
	parked string // 被第三方挪走的原件所在路径
	calls  int    // removeSrc 实际被调用的次数
}

// hijackAfterCopy 在复制完成后把 srcPath 顶替成第三方的新文件（原子改名 + 落新内容），
// 返回记录"删源是否真的发生"的状态。
//
// parked 里是**被第三方挪走的原件**，srcPath 里是第三方的新文件——
// 两者都不是应用该删的东西，都必须在用例结束后仍然存在。
func hijackAfterCopy(t *testing.T, srcPath, thirdPartyContent string) *hijackState {
	t.Helper()
	st := &hijackState{}
	origCopy := copyVerifyFile
	copyVerifyFile = func(src, dst string, fi os.FileInfo) error {
		if err := origCopy(src, dst, fi); err != nil {
			return err
		}
		st.parked = src + ".third-party-parked"
		if err := os.Rename(src, st.parked); err != nil {
			return err
		}
		return os.WriteFile(src, []byte(thirdPartyContent), 0o644)
	}
	t.Cleanup(func() { copyVerifyFile = origCopy })

	origRemove := removeSrc
	removeSrc = func(p string) error {
		st.calls++
		return origRemove(p)
	}
	t.Cleanup(func() { removeSrc = origRemove })
	return st
}

func TestMoveFileCrossVolumeDoesNotDeleteReplacedSource(t *testing.T) {
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	dstDir := filepath.Join(dir, "dst")
	for _, d := range []string{srcDir, dstDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	src := filepath.Join(srcDir, "victim.bin")
	payload := []byte("ORIGINAL-CONTENT-THAT-WAS-COPIED")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	forceCrossVolumeRename(t)
	const thirdParty = "someone else's freshly written file"
	st := hijackAfterCopy(t, src, thirdParty)

	_, err := MoveFile(src, dstDir)
	if err == nil {
		t.Fatal("复制期间源被第三方顶替，MoveFile 却报告成功：删掉的是第三方文件，账本还会记 done（AS-H4）")
	}
	if !strings.Contains(err.Error(), "源") || !strings.Contains(err.Error(), "替换") {
		t.Fatalf("错误应说明「源在复制期间被替换」，实际: %v", err)
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
	// 第三方挪走的原件也在原目录里，同样不该被本应用删掉
	if _, lerr := os.Lstat(st.parked); lerr != nil {
		t.Fatalf("被顶替走的原件（%s）消失: %v", st.parked, lerr)
	}
}

// TestMoveFileCrossVolumeRemovesUnchangedSource 负控制：源在复制窗口内没被动过时，
// 删源必须照常发生（守卫不得过严，否则跨卷移动退化成复制，占用翻倍）。
func TestMoveFileCrossVolumeRemovesUnchangedSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "s.bin")
	dstDir := filepath.Join(dir, "d")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatal(err)
	}
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

	dst, err := MoveFile(src, dstDir)
	if err != nil {
		t.Fatalf("未变动的源应正常完成跨卷移动: %v", err)
	}
	if calls != 1 {
		t.Fatalf("删源应恰好调用一次，实际 %d 次", calls)
	}
	if _, err := os.Lstat(src); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("跨卷移动后源应已删除，实际 Lstat=%v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "steady content here" {
		t.Fatalf("目标内容不符: %q", got)
	}
}

// TestUndoTrashCrossVolumeDoesNotDeleteReplacedDest 同一缺陷在回收站回填路径上的形态
// （undo.go 跨卷恢复：copyVerify 之后按路径删回收站侧文件）。
func TestUndoTrashCrossVolumeDoesNotDeleteReplacedDest(t *testing.T) {
	dir := t.TempDir()
	rbin := filepath.Join(dir, "rbin")
	orig := filepath.Join(dir, "orig.bin")
	if err := os.MkdirAll(rbin, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(rbin, "victim.bin")
	payload := []byte("TRASHED-ORIGINAL-PAYLOAD!!!")
	if err := os.WriteFile(dest, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	it := UndoItem{
		Kind:     "trash",
		OrigPath: orig,
		DestPath: dest,
		Hash:     hashOfContent(t, payload),
		Size:     uint64(len(payload)),
	}

	forceCrossVolumeRename(t)
	st := hijackAfterCopy(t, dest, "another tenant in this path")

	if _, err := UndoOne(it); err == nil {
		t.Fatal("回收站侧文件在复制期间被顶替，回撤却报告成功——按路径盲删会清掉第三方文件（AS-H4 同型）")
	}
	if st.calls != 0 {
		t.Fatalf("守卫触发后不得删回收站侧路径，实际删源调用 %d 次", st.calls)
	}
	got, lerr := os.ReadFile(dest)
	if lerr != nil {
		t.Fatalf("第三方文件被回撤流程删掉了: %v", lerr)
	}
	if string(got) != "another tenant in this path" {
		t.Fatalf("第三方文件内容被改动: %q", got)
	}
}
