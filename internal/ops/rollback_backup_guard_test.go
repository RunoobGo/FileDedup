package ops

// M20（2026-09-21，设计稿 §8）：合并回滚的**还原**一侧。
//
// 终局复核失败之后、把备份搬回 dup 之前，这一段里没有任何证据说明 backup 位上
// 还是本次操作的原文件。第三方在"步骤 3 改名成功"与"回滚"之间把它换掉
// （同步盘落子），回滚分支就会把**那个陌生文件**搬进 dup 位，还把位置说成
// "原文件保留在 …"——用户被指向一个不属于他的文件。
//
// 判据：搬运前先证明 backup 位上还是 dupID 那份文件；证明不了就一个字节都不搬、
// 不挪，只把位置报出来，且文案不许把陌生文件称作原文件。
//
// 两条回滚分支（步骤 5 复核失败 / 步骤 4 落位改名失败）都要有这条前置：
// 前者原先在两个文件里逐字重复了两份（I5 的样本），本项一并上收到 merge_guard.go。

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const backupVictimData = "THIRD-PARTY-REPLACED-THE-BACKUP"

// installBackupSwapOnLanding 在"tmp → dup 落位改名"这一刻做两件事（同一窗口）：
//   - tamperLanding=true 时让这次落位**名不符实**：落一份逐字节副本而不是链接，
//     于是步骤 5 的终局复核必失败（这就是"复核失败"的构造方式）；
//   - 随后把 backup 位换成第三方文件。
//
// 时机必然落在 backupOwnershipStill 复核（步骤 3 之后那一行）**之后**，
// 因此不会被 M1 的窗口 A 守卫先拦下——这正是 M20 说的"守卫之间的空档"。
func installBackupSwapOnLanding(t *testing.T, dup string, tamperLanding bool) {
	t.Helper()
	orig := hardlinkRename
	tmp, backup := dup+FddTempSuffix, dup+FddOldSuffix
	var done bool
	hardlinkRename = func(oldp, newp string) error {
		if oldp == tmp && newp == dup {
			if tamperLanding {
				b, err := os.ReadFile(oldp)
				if err != nil {
					return err
				}
				if err := os.WriteFile(newp, b, 0o644); err != nil {
					return err
				}
				if err := os.Remove(oldp); err != nil {
					return err
				}
			} else if err := orig(oldp, newp); err != nil {
				return err
			}
			done = true
			if err := os.Remove(backup); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(backup, []byte(backupVictimData), 0o644); err != nil {
				t.Fatal(err)
			}
			return nil
		}
		return orig(oldp, newp)
	}
	t.Cleanup(func() { hardlinkRename = orig })
	t.Cleanup(func() {
		if !done {
			t.Error("前置条件未触发：落位改名从未发生，本用例没有验证任何东西")
		}
	})
}

// installBackupSwapOnFailedLanding 在"落位改名失败"的**同一刻**把 backup 换成
// 第三方文件：步骤 4 的失败分支同样会在无证据的情况下把 backup 搬回 dup。
func installBackupSwapOnFailedLanding(t *testing.T, dup string) {
	t.Helper()
	orig := hardlinkRename
	tmp, backup := dup+FddTempSuffix, dup+FddOldSuffix
	var done bool
	hardlinkRename = func(oldp, newp string) error {
		if oldp == tmp && newp == dup {
			done = true
			if err := os.Remove(backup); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(backup, []byte(backupVictimData), 0o644); err != nil {
				t.Fatal(err)
			}
			return errors.New("模拟：落位改名失败")
		}
		return orig(oldp, newp)
	}
	t.Cleanup(func() { hardlinkRename = orig })
	t.Cleanup(func() {
		if !done {
			t.Error("前置条件未触发：落位改名从未发生，本用例没有验证任何东西")
		}
	})
}

// assertBackupVictimUntouched 断言：那个陌生文件原地不动、也没被搬到 dup 位，
// 且文案不把它称作"原文件保留在 …"。
func assertBackupVictimUntouched(t *testing.T, f mergeFixture, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("backup 位被第三方顶替时必须上报错误，不能返回 nil")
	}
	backup := f.dup + FddOldSuffix
	b, rerr := os.ReadFile(backup)
	if rerr != nil || string(b) != backupVictimData {
		t.Fatalf("backup 位的陌生文件被搬走或改写（M20）：read=%q err=%v", b, rerr)
	}
	if b, rerr := os.ReadFile(f.dup); rerr == nil && string(b) == backupVictimData {
		t.Fatalf("%s 位上出现了那个陌生文件：回滚把它搬了家", f.dup)
	}
	msg := err.Error()
	if !strings.Contains(msg, backup) {
		t.Fatalf("错误未报出 backup 位的位置，用户无从核对: %v", err)
	}
	if !strings.Contains(msg, "已不是本次操作的原文件") {
		t.Fatalf("错误未说明该位置的文件已不是原文件: %v", err)
	}
	if strings.Contains(msg, "原文件保留在 ") {
		t.Fatalf("文案把陌生文件称作「原文件保留在 …」——正是 M20 登记的伤害: %v", err)
	}
}

// ---- 步骤 5：终局复核失败 ----

func TestHardlinkMergeRollbackRefusesReplacedBackup(t *testing.T) {
	f := newMergeFixture(t)
	installBackupSwapOnLanding(t, f.dup, true)

	err := HardlinkMerge(f.keep, f.dup, f.keepID, f.dupID)
	assertBackupVictimUntouched(t, f, err)
	// dup 位上那个"名不符实"的对象必须原样留在那里：它已被判定不能证明归属，
	// 我们既不删它、也不把它挪走。
	if b, e := os.ReadFile(f.dup); e != nil || string(b) != "KEEP-BYTES-shared" {
		t.Fatalf("dup 位上校验未通过的对象被动过（应原样保留）: %q err=%v", b, e)
	}
}

func TestSymlinkMergeRollbackRefusesReplacedBackup(t *testing.T) {
	f := newMergeFixture(t)
	requireSymlinkSupport(t, filepath.Dir(f.dup))
	installBackupSwapOnLanding(t, f.dup, true)

	err := SymlinkMerge(f.keep, f.dup, f.keepID, f.dupID)
	assertBackupVictimUntouched(t, f, err)
	if b, e := os.ReadFile(f.dup); e != nil || string(b) != "KEEP-BYTES-shared" {
		t.Fatalf("dup 位上校验未通过的对象被动过（应原样保留）: %q err=%v", b, e)
	}
}

// ---- 步骤 4：落位改名失败 ----

func TestHardlinkMergeSwapFailureRefusesReplacedBackup(t *testing.T) {
	f := newMergeFixture(t)
	installBackupSwapOnFailedLanding(t, f.dup)

	err := HardlinkMerge(f.keep, f.dup, f.keepID, f.dupID)
	assertBackupVictimUntouched(t, f, err)
	// 步骤 4 失败 ⇒ dup 位本应为空，现在必须仍然是空的（陌生文件没被搬进来）
	if _, e := os.Lstat(f.dup); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("dup 位不该有文件: err=%v", e)
	}
	// 临时链接（我们自己的）照旧要清掉
	if _, e := os.Lstat(f.dup + FddTempSuffix); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("临时槽位残留: %v", e)
	}
}

func TestSymlinkMergeSwapFailureRefusesReplacedBackup(t *testing.T) {
	f := newMergeFixture(t)
	requireSymlinkSupport(t, filepath.Dir(f.dup))
	installBackupSwapOnFailedLanding(t, f.dup)

	err := SymlinkMerge(f.keep, f.dup, f.keepID, f.dupID)
	assertBackupVictimUntouched(t, f, err)
	if _, e := os.Lstat(f.dup); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("dup 位不该有文件: err=%v", e)
	}
	if _, e := os.Lstat(f.dup + FddTempSuffix); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("临时槽位残留: %v", e)
	}
}
