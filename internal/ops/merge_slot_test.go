package ops

// M6（2026-09-20 全仓审计 §五 6）：合并流程进门就对两个临时名槽位做无条件
// `_ = os.Remove(tmp)` / `_ = os.Remove(backup)`。
//
// 于是"真以 .fdd-old / .fdd-tmp 结尾的用户文件"会被当成应用残留**永久删除**
// ——用户从未授权我们处置它，而且这个名字扫描器还会忽略，删完连痕迹都找不到。
//
// 修法口径：槽位空闲 → 直接用；槽位被占但能**正面证明**是上一次运行留下的
// 我们自己的对象（与 keep / dup 同一身份，删名字不丢数据）→ 清掉再用；
// 证明不了 → 显式失败，一个字节都不碰。

import (
	"os"
	"strings"
	"testing"
)

// plantForeignAtSlot 在某个临时名槽位放一个**内容不同**的第三方文件。
func plantForeignAtSlot(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(victimData), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertForeignSlotIntact(t *testing.T, slot, dup string) {
	t.Helper()
	got, err := os.ReadFile(slot)
	if err != nil {
		t.Fatalf("占位文件被删除了（M6 未修）: %v", err)
	}
	if string(got) != victimData {
		t.Fatalf("占位文件内容被改写: %q", got)
	}
	if b, err := os.ReadFile(dup); err != nil || string(b) != "ORIGINAL-DUP-DATA" {
		t.Fatalf("dup 被连带改动了: %q err=%v", b, err)
	}
}

func TestHardlinkMergeRefusesForeignBackupSlot(t *testing.T) {
	f := newMergeFixture(t)
	backup := f.dup + FddOldSuffix
	plantForeignAtSlot(t, backup)

	err := HardlinkMerge(f.keep, f.dup, f.keepID, f.dupID)
	if err == nil {
		t.Fatal("备份槽位被用户文件占用时合并照样成功：旧代码无条件 os.Remove(backup)")
	}
	if !strings.Contains(err.Error(), backup) {
		t.Fatalf("错误未指出冲突文件位置，用户无从处理: %v", err)
	}
	assertForeignSlotIntact(t, backup, f.dup)
}

func TestHardlinkMergeRefusesForeignTmpSlot(t *testing.T) {
	f := newMergeFixture(t)
	tmp := f.dup + FddTempSuffix
	plantForeignAtSlot(t, tmp)

	err := HardlinkMerge(f.keep, f.dup, f.keepID, f.dupID)
	if err == nil {
		t.Fatal("临时槽位被用户文件占用时合并照样成功：旧代码无条件 os.Remove(tmp)")
	}
	if !strings.Contains(err.Error(), tmp) {
		t.Fatalf("错误未指出冲突文件位置，用户无从处理: %v", err)
	}
	assertForeignSlotIntact(t, tmp, f.dup)
}

func TestSymlinkMergeRefusesForeignBackupSlot(t *testing.T) {
	f := newMergeFixture(t)
	backup := f.dup + FddOldSuffix
	plantForeignAtSlot(t, backup)

	err := SymlinkMerge(f.keep, f.dup, f.keepID, f.dupID)
	if err == nil {
		t.Fatal("软链接合并同样把用户文件当残留删掉了")
	}
	if !strings.Contains(err.Error(), backup) {
		t.Fatalf("错误未指出冲突文件位置: %v", err)
	}
	assertForeignSlotIntact(t, backup, f.dup)
}

// TestSymlinkMergeTmpSlotRequiresSymlinkProof 钉住"证明方式必须与操作形态匹配"：
// 软链接合并留下的 tmp 是**指向 keep 的链接**。槽位上若是一个普通硬链接
// （哪怕指的就是 keep），也不符合本路径的取证方式，不能凭"看着像残留"就删。
func TestSymlinkMergeTmpSlotRequiresSymlinkProof(t *testing.T) {
	f := newMergeFixture(t)
	tmp := f.dup + FddTempSuffix
	if err := os.Link(f.keep, tmp); err != nil {
		t.Skipf("本卷不支持硬链接，无法构造「同 inode 但非链接」的槽位: %v", err)
	}
	err := SymlinkMerge(f.keep, f.dup, f.keepID, f.dupID)
	if err == nil {
		t.Fatal("软链接合并的 tmp 槽位未按「链接指向 keep」取证就放行")
	}
	if _, err := os.Lstat(tmp); err != nil {
		t.Fatalf("槽位上的对象被删掉了: %v", err)
	}
	if b, err := os.ReadFile(f.dup); err != nil || string(b) != "ORIGINAL-DUP-DATA" {
		t.Fatalf("dup 被连带改动了: %q err=%v", b, err)
	}
}

// TestHardlinkMergeReclaimsOwnCrashResidue 反向守卫：合法的上次残留必须照常
// 回收，否则这次修复会把"崩溃后重做同一个文件"堵死。
func TestHardlinkMergeReclaimsOwnCrashResidue(t *testing.T) {
	f := newMergeFixture(t)
	tmp := f.dup + FddTempSuffix
	backup := f.dup + FddOldSuffix
	// 上次运行留下：指向 keep 的临时硬链接 + 与原 dup 同身份的备份。
	if err := os.Link(f.keep, tmp); err != nil {
		t.Skipf("本卷不支持硬链接: %v", err)
	}
	if err := os.Link(f.dup, backup); err != nil {
		t.Skipf("本卷不支持硬链接: %v", err)
	}

	if err := HardlinkMerge(f.keep, f.dup, f.keepID, f.dupID); err != nil {
		t.Fatalf("合法残留未被回收，合并被自己的守卫堵死: %v", err)
	}
	assertNoResidue(t, f.dup)
	ki, err1 := os.Stat(f.keep)
	di, err2 := os.Stat(f.dup)
	if err1 != nil || err2 != nil {
		t.Fatalf("stat: %v / %v", err1, err2)
	}
	if !os.SameFile(ki, di) {
		t.Fatal("合并未到位：dup 与 keep 不是同一物理文件")
	}
}
