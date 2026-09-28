package ops

// M344（2026-09-28 第七轮补审 P0）：合并的**落位改名**不得静默覆盖第三方文件。
//
// 改前形状：`HardlinkMerge` / `SymlinkMerge` 把 dup 改名成 backup（腾空）之后，
// 用 `hardlinkRename(tmp, dup)` 落位。而 os.Rename 对已存在的普通文件是**静默替换**
// ——第三方在"腾空 ⇒ 落位"这个窗口里落子，会被连名字带内容一起顶掉；
// 随后终局复核（verifyHardlinked / verifySymlinked）与账本都指向我们自己放上去的
// 内容，**全过**，用户得到一条 done 且零告警 ⇒ 第三方文件凭空消失且无人知晓。
//
// 修法：与 M48 那三处（restoreInPlace / undoSymlink / abandonForeignBackup）同一条
// 纪律——落位前先用 claimExact 抢占这个名字；抢不到就放弃合并（一个字节都不碰）。
//
// 构造方式：把 openExclusive（抢名的唯一落点，已是包级 var）换成"先把第三方文件
// 落到 dup 位、再返回 EEXIST"，即让抢占在**真实的失败分支**上发生。

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// occupyThenFail 让"抢占 dup 位"这一刻变成：dup 位已经有第三方的东西。
func occupyThenFail(t *testing.T, dup string) {
	t.Helper()
	orig := openExclusive
	openExclusive = func(path string, flag int, perm os.FileMode) (*os.File, error) {
		if path == dup {
			if err := os.WriteFile(dup, []byte(victimData), 0o644); err != nil {
				t.Errorf("夹具：落第三方文件失败 %v", err)
			}
			return nil, &fs.PathError{Op: "open", Path: path, Err: syscall.EEXIST}
		}
		return orig(path, flag, perm)
	}
	t.Cleanup(func() { openExclusive = orig })
}

func assertVictimSurvived(t *testing.T, path string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("第三方文件读不到了（被顶掉了？）: %v", err)
	}
	if string(got) != victimData {
		t.Fatalf("dup 位的内容 = %q, want %q：第三方的文件被我们的落位改名覆盖",
			string(got), victimData)
	}
}

func TestHardlinkLandingDoesNotOverwriteThirdParty(t *testing.T) {
	f := newMergeFixture(t)
	dup := f.dup
	occupyThenFail(t, dup)

	err := HardlinkMerge(f.keep, f.dup, f.keepID, f.dupID)
	if err == nil {
		t.Fatal("dup 位被第三方占用时仍返回成功：合并把别人的文件顶掉了还记成 done")
	}
	assertVictimSurvived(t, dup)
	// 放弃时不得留下我们自己的临时链接（tmp 已被清理，dup 位是第三方的）
	if _, err := os.Lstat(dup + FddTempSuffix); err == nil {
		t.Error("放弃合并后临时链接残留：下一轮会被 claimSlot 当成需要清的槽位")
	}
}

func TestSymlinkLandingDoesNotOverwriteThirdParty(t *testing.T) {
	f := newMergeFixture(t)
	dup := f.dup
	occupyThenFail(t, dup)

	err := SymlinkMerge(f.keep, f.dup, f.keepID, f.dupID)
	if err == nil {
		t.Fatal("dup 位被第三方占用时仍返回成功：合并把别人的文件顶掉了还记成 done")
	}
	assertVictimSurvived(t, dup)
}

// 负控制：抢占正常成功时，合并必须照旧完成（不能因为加了抢占就把正常路径打死）。
func TestLandingStillSucceedsWhenSlotFree(t *testing.T) {
	f := newMergeFixture(t)
	if err := HardlinkMerge(f.keep, f.dup, f.keepID, f.dupID); err != nil {
		t.Fatalf("空位下的正常合并被抢占逻辑打死了: %v", err)
	}
	// 备份（原独立副本）已被删除，dup 位现在与 keep 同一份数据
	if _, err := os.Lstat(f.dup + FddOldSuffix); err == nil {
		t.Error("合并成功后备份仍残留")
	}
	if filepath.Base(f.dup) != "dup.bin" {
		t.Fatal("夹具前提变了")
	}
}
