package ops

// M320（OPS-46，2026-09-28 第六轮全量审查）：`moveIntoTrash` 把 `renameFile` 的
// 错误**整个丢掉**，只要改名不成功就无条件退化成"复制一份进回收站 + 删源"。
//
// move.go 的同形位置一直写着 `else if !isCrossDevice(err) { 失败上抛 }`（只有 EXDEV
// 允许退化成复制，I5 同源判据），trash 这条腿没有。后果不是丢数据（M207 之后删源前
// 已有身份复核），而是**归因被吞**：回收站属主不对、目标卷只读、配额满、权限不足这些
// 最常见的 dst 侧故障，用户看到的是"进了回收站"（一次跨卷复制），真实原因从没出现过；
// 若随后删源失败，报的还是"跨卷入回收站时删源失败"——那句在此刻根本不是跨卷。
//
// 时序由既有包级接缝 `renameFile` 钉死（本机造不出第二个真实挂载卷），注入桩返回的
// 错误形状与生产一致（`*os.LinkError`，`isCrossDevice` 只认这一种）。
// 无 build tag：两条判据都靠注入，三腿同跑（H6 惯例）。
// 改前红可得：第一条用例只引用改前就有的符号，改前会走复制腿并成功返回 nil（源被删）
// ⇒ "返回错误 + 源仍在"两格当场红；第二条是防修法过头的负控制（M320-b 的红靶）。

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// forceRenameFailure 让 renameFile 稳定返回指定错误（不真动盘）。
func forceRenameFailure(t *testing.T, err error) {
	t.Helper()
	orig := renameFile
	renameFile = func(oldPath, newPath string) error {
		return &os.LinkError{Op: "rename", Old: oldPath, New: newPath, Err: err}
	}
	t.Cleanup(func() { renameFile = orig })
}

// m320TrashDst 造出回收站 files/ 那一层并返回条目落点：生产的 dst 由 uniqueXDG
// 在已存在的 filesDir 里选出，夹具若不铺这层目录，复制腿会先撞 ENOENT，
// "改前会退化成复制"这一格就被夹具自己挡掉了。
func m320TrashDst(t *testing.T, dir string) string {
	t.Helper()
	files := filepath.Join(dir, "trash")
	if err := os.MkdirAll(files, 0o700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(files, "victim.bin")
}

func TestM320TrashRenamePermFailureDoesNotFallBackToCopy(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "victim.bin")
	const payload = "MUST-NOT-BE-COPIED-INTO-TRASH"
	if err := os.WriteFile(src, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := m320TrashDst(t, dir)
	forceRenameFailure(t, syscall.EPERM)

	err := moveIntoTrash(src, dst)
	if err == nil {
		t.Fatal("改名因 EPERM 失败却报告成功：走了复制腿，源已被删（M320）")
	}
	if !strings.Contains(err.Error(), "rename") && !strings.Contains(err.Error(), "回收站") {
		t.Errorf("错误应带上改名失败的原委而不是被吞掉: %v", err)
	}
	if !strings.Contains(err.Error(), "permission denied") && !strings.Contains(err.Error(), "operation not permitted") {
		t.Errorf("错误必须保住底层 errno（用户据此才能判断是权限/只读/配额）: %v", err)
	}
	if b, e := os.ReadFile(src); e != nil || string(b) != payload {
		t.Errorf("现场必须一步未动，源读得 %q err=%v", b, e)
	}
	if _, e := os.Lstat(dst); !os.IsNotExist(e) {
		t.Errorf("拦截腿不得在回收站侧留下任何东西: %v", e)
	}
}

// 负控制：真正的跨卷（EXDEV）仍必须退复制腿——修法修过头会把跨卷回收站整个打死。
func TestM320TrashStillCopiesOnRealCrossDevice(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "victim.bin")
	const payload = "CROSS-VOLUME-PAYLOAD-THAT-SHOULD-BE-COPIED"
	if err := os.WriteFile(src, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := m320TrashDst(t, dir)
	forceRenameFailure(t, syscall.EXDEV)

	if err := moveIntoTrash(src, dst); err != nil {
		t.Fatalf("EXDEV 应照旧退化为复制+删源，实得: %v", err)
	}
	if b, e := os.ReadFile(dst); e != nil || string(b) != payload {
		t.Errorf("回收站侧副本内容不符: %q err=%v", b, e)
	}
	if _, e := os.Lstat(src); !os.IsNotExist(e) {
		t.Errorf("复制成功后源应已删除: %v", e)
	}
}
