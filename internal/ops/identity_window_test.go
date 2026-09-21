package ops

// M1 + M3（2026-09-21，第 7 篇 §五 1/3）：合并流程里"复核之后、动手之前"的窗口。
//
// M1 的症状不是"没复核"，而是**复核的位置不对**：
//
//	identityStill(dup, dupID) ✅ → [窗口] → rename(dup → backup) → rename(tmp → dup)
//	→ 终局复核 ✅ → os.Remove(backup)
//
// 窗口里第三方把新文件放进 dup 位（同步盘落一个同名文件、下载器原子改名进来，
// 都是这个时序）时，被 rename 成 backup 的是**那个第三方文件**；后续每一步复核
// 看的都是 keep 与链接，全都成立，最后 workTempRemove(backup) 把它当残留**永久删除**。
// 数据丢的还是别人的文件——本应用从未获授权处置它。
//
// M3 的症状是同一段流程的失败分支：步骤 4 改名失败时用
// `_ = hardlinkRename(backup, dup)` 还原，还原也失败就只返回裸 err——
// 用户数据留在 dup+".fdd-old"，而这个名字扫描器按 worktemp.IsTempName 忽略，
// 于是"用户的文件在应用视角里静默消失"。
//
// 两条都要求：宁可显式失败，也不替第三方删文件、不把用户的文件变成隐形残留。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/fsid"
)

const victimData = "THIRD-PARTY-FILE-MUST-SURVIVE"

// mergeFixture 铺出 keep + dup，并返回**已解析**的身份（未解析的卷上
// identityStill 恒真，测不出守卫，只能跳过）。
type mergeFixture struct {
	keep, dup       string
	keepID, dupID   fsid.ID
	dupWasRemovable bool
}

func newMergeFixture(t *testing.T) mergeFixture {
	t.Helper()
	dir := t.TempDir()
	f := mergeFixture{
		keep: filepath.Join(dir, "keep.bin"),
		dup:  filepath.Join(dir, "dup.bin"),
	}
	if err := os.WriteFile(f.keep, []byte("KEEP-BYTES-shared"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.dup, []byte("ORIGINAL-DUP-DATA"), 0o644); err != nil {
		t.Fatal(err)
	}
	var err error
	if f.keepID, err = fsid.FromPathNoFollow(f.keep); err != nil {
		t.Fatalf("keep 身份不可解析: %v", err)
	}
	if f.dupID, err = fsid.FromPathNoFollow(f.dup); err != nil {
		t.Fatalf("dup 身份不可解析: %v", err)
	}
	if !f.keepID.Resolved || !f.dupID.Resolved {
		t.Skipf("本卷不提供稳定身份索引（keep.Resolved=%v dup.Resolved=%v）：守卫无从比对",
			f.keepID.Resolved, f.dupID.Resolved)
	}
	return f
}

// plantForeignAtDup 在"复核已通过、dup 即将被改名成 backup"这一刻，
// 把 dup 位置换成一个第三方文件。返回该文件的身份，供断言"没被删"。
//
// 顶替走 swap_fixture_test.go 的原子改名（原实现是 Remove + WriteFile，
// 在会回收 inode 号的卷上让顶替者拿到刚释放的那个号，2026-09-22 linux 腿
// 因此红在守卫上而不是红在夹具前提上——见 §21.1）。
func plantForeignAtDup(t *testing.T, dup string) fsid.ID {
	t.Helper()
	victim := swapOutAt(t, dup, []byte(victimData))
	if !victim.Resolved {
		t.Skip("第三方文件身份不可解析，无法断言未被删除")
	}
	return victim
}

// ---- M1 ①：backup 位上是第三方文件时，早退守卫必须放弃合并并原样归还 ----

func TestHardlinkMergeDoesNotDeleteForeignBackup(t *testing.T) {
	f := newMergeFixture(t)
	installForeignSwapOnFirstRename(t, f.dup)

	err := HardlinkMerge(f.keep, f.dup, f.keepID, f.dupID)
	assertForeignBackupAborted(t, f, err)
}

func TestSymlinkMergeDoesNotDeleteForeignBackup(t *testing.T) {
	f := newMergeFixture(t)
	requireSymlinkSupport(t, filepath.Dir(f.dup))
	installForeignSwapOnFirstRename(t, f.dup)

	err := SymlinkMerge(f.keep, f.dup, f.keepID, f.dupID)
	assertForeignBackupAborted(t, f, err)
}

// installForeignSwapOnFirstRename 把第三方文件塞进 dup 位的时机，钉在
// "dup→backup 这次改名的入口"——即所有既有复核**之后**、真正的破坏性动作之前。
// 这正是 M1 描述的窗口本身，不是它的近似。
func installForeignSwapOnFirstRename(t *testing.T, dup string) {
	t.Helper()
	orig := hardlinkRename
	backup := dup + FddOldSuffix
	var done bool
	hardlinkRename = func(oldp, newp string) error {
		if !done && oldp == dup && newp == backup {
			done = true
			plantForeignAtDup(t, dup)
		}
		return orig(oldp, newp)
	}
	t.Cleanup(func() { hardlinkRename = orig })
	if !done {
		t.Cleanup(func() {
			if !done {
				t.Error("前置条件未触发：dup→backup 的改名从未发生，本用例没有验证任何东西")
			}
		})
	}
}

func assertForeignBackupAborted(t *testing.T, f mergeFixture, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("第三方文件被合并流程吸收了（返回 nil）：守卫没生效")
	}
	// ① 第三方文件必须还活着，且回到它原本的位置
	vb, readErr := os.ReadFile(f.dup)
	if readErr != nil {
		t.Fatalf("第三方文件在 dup 位找不到了（被删或被挪走？）: %v", readErr)
	}
	if string(vb) != victimData {
		t.Fatalf("dup 位内容不是第三方文件（%q）：现场被改动", vb)
	}
	// ② backup 位不该再有东西（既不该留下我们的残留，也不该留着别人的文件等删）
	if _, e := os.Lstat(f.dup + FddOldSuffix); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("backup 位仍有文件（err=%v）：M1 的删除点就在这里", e)
	}
	// ③ 临时名不得残留
	if _, e := os.Lstat(f.dup + FddTempSuffix); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("临时硬链接/软链接残留（err=%v）", e)
	}
	// ④ dup 位不该被换成链接：合并必须**没有**发生
	if li, e := os.Lstat(f.dup); e == nil && li.Mode()&os.ModeSymlink != 0 {
		t.Fatal("第三方文件被替换成了符号链接：合并被拦截却仍然动了 dup 位")
	}
	// ⑤ 报错文案必须说清"是谁的文件被保住"，否则用户以为是自己没合并成功
	if !strings.Contains(err.Error(), "第三方") {
		t.Fatalf("错误文案未说明是第三方文件顶替（用户会以为只是普通失败）: %v", err)
	}
	// ⑥ 保留源不受影响
	if b, e := os.ReadFile(f.keep); e != nil || string(b) != "KEEP-BYTES-shared" {
		t.Fatalf("保留源被改动: %q %v", b, e)
	}
}

// ---- M1 ②：晚到的顶替——删 backup 前必须最后一次核对归属 ----

func TestHardlinkMergeRechecksBackupOwnershipBeforeDelete(t *testing.T) {
	f := newMergeFixture(t)
	installForeignSwapOnBackup(t, f.dup, f.dupID)

	err := HardlinkMerge(f.keep, f.dup, f.keepID, f.dupID)
	assertForeignKeptAsWarning(t, f, err)
}

func TestSymlinkMergeRechecksBackupOwnershipBeforeDelete(t *testing.T) {
	f := newMergeFixture(t)
	requireSymlinkSupport(t, filepath.Dir(f.dup))
	installForeignSwapOnBackup(t, f.dup, f.dupID)

	err := SymlinkMerge(f.keep, f.dup, f.keepID, f.dupID)
	assertForeignKeptAsWarning(t, f, err)
}

// installForeignSwapOnBackup 在"链接已落位、backup 尚未删除"之间，把 backup
// 换成另一个第三方文件（前一道守卫看不见这种晚到的顶替）。
func installForeignSwapOnBackup(t *testing.T, dup string, dupID fsid.ID) {
	t.Helper()
	orig := hardlinkRename
	tmp, backup := dup+FddTempSuffix, dup+FddOldSuffix
	var done bool
	hardlinkRename = func(oldp, newp string) error {
		if err := orig(oldp, newp); err != nil {
			return err
		}
		if !done && oldp == tmp && newp == dup {
			done = true
			swapInAt(t, backup, dupID, []byte(victimData))
			if identityStill(backup, dupID) {
				t.Fatal("前置条件不成立：换进去的文件仍被判定为本次操作的 dup，断言将失去意义")
			}
		}
		return nil
	}
	t.Cleanup(func() { hardlinkRename = orig })
	if !done {
		t.Cleanup(func() {
			if !done {
				t.Error("前置条件未触发：tmp→dup 的落位改名从未发生，本用例没有验证任何东西")
			}
		})
	}
}

func assertForeignKeptAsWarning(t *testing.T, f mergeFixture, err error) {
	t.Helper()
	// 合并本身已经成功（dup 与 keep 同一份数据 / 链接指向 keep），所以不能判失败：
	// 上层要按"成功 + 告警"记账（与 ResidueError 同一条路），否则用户会以为
	// 没生效而反复重试。
	var residue *ResidueError
	if !errors.As(err, &residue) {
		t.Fatalf("期望归类为「已完成但有残留」（*ResidueError），实际 %T: %v", err, err)
	}
	vb, e := os.ReadFile(f.dup + FddOldSuffix)
	if e != nil {
		t.Fatalf("backup 位的第三方文件被删了: %v", e)
	}
	if string(vb) != victimData {
		t.Fatalf("backup 位内容被改动: %q", vb)
	}
	msg := residue.Error()
	if strings.Contains(msg, "请手动删除") {
		t.Fatalf("文案把一个不属于本次操作的文件说成「请手动删除」的残留（这会引导用户删掉别人的文件）: %s", msg)
	}
	if !strings.Contains(msg, "第三方") {
		t.Fatalf("文案未说明该文件不属于本次操作: %s", msg)
	}
}

// ---- M3：还原也失败时，必须把用户数据的位置说出来 ----

func TestHardlinkMergeRestoreFailureReportsDataLocation(t *testing.T) {
	f := newMergeFixture(t)
	installDoubleRenameFailure(t, f.dup)

	err := HardlinkMerge(f.keep, f.dup, f.keepID, f.dupID)
	assertRestoreFailureReported(t, f, err)
}

func TestSymlinkMergeRestoreFailureReportsDataLocation(t *testing.T) {
	f := newMergeFixture(t)
	requireSymlinkSupport(t, filepath.Dir(f.dup))
	installDoubleRenameFailure(t, f.dup)

	err := SymlinkMerge(f.keep, f.dup, f.keepID, f.dupID)
	assertRestoreFailureReported(t, f, err)
}

// installDoubleRenameFailure 构造最坏时序：dup 已成功挪到 backup，
// 落位改名（tmp→dup）失败，连"把原文件还原回 dup"也失败。
func installDoubleRenameFailure(t *testing.T, dup string) {
	t.Helper()
	orig := hardlinkRename
	tmp, backup := dup+FddTempSuffix, dup+FddOldSuffix
	failure := errors.New("模拟：目标卷繁忙")
	hardlinkRename = func(oldp, newp string) error {
		switch {
		case oldp == tmp && newp == dup:
			return failure
		case oldp == backup && newp == dup:
			return fmt.Errorf("模拟：还原也失败: %w", failure)
		}
		return orig(oldp, newp)
	}
	t.Cleanup(func() { hardlinkRename = orig })
}

func assertRestoreFailureReported(t *testing.T, f mergeFixture, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("还原失败必须上报，不能返回 nil")
	}
	backup := f.dup + FddOldSuffix
	// 数据确实还在那里（先确认前提，再断言文案）
	b, e := os.ReadFile(backup)
	if e != nil {
		t.Fatalf("前置条件不成立：backup 不在 %s: %v", backup, e)
	}
	if string(b) != "ORIGINAL-DUP-DATA" {
		t.Fatalf("backup 内容不是原数据: %q", b)
	}
	// ★ 本用例的核心：dup 位现在是**空的**，用户数据只在 backup，
	// 而 backup 这个名字扫描器会忽略——不写清位置，用户找不到自己的文件。
	msg := err.Error()
	if !strings.Contains(msg, backup) {
		t.Fatalf("错误未指出数据留在 %s（扫描器忽略该名字，用户无从找回）: %v", backup, err)
	}
	if !strings.Contains(msg, "还原") {
		t.Fatalf("错误未说明还原失败（只报了改名失败，用户会以为文件还在原位）: %v", err)
	}
}
