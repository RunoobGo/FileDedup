package ops

// M48（2026-09-22 裁定"做两处，第三处写明窗口"，设计稿 §27.6）：
// 回撤链里"检查/腾空 与 改名上位"之间的两步窗口，改成 O_EXCL 先占住名字。
//
// 两处形状相同、代码不同，各钉各的：
//   undoSymlink           —— 删掉我们建的软链接 → 把备份改名回原位；
//   abandonForeignBackup   —— 确认 dup 位空着 → 把第三方文件改名回 dup 位。
//
// 钩子（move.go 的 beforeClaimRename，生产恒 nil）在"名字刚空出来"那一刻让第三方
// **被动落子**（同步盘/下载器把版本写回来的形态）。钉的是：那次落子必须撞 EEXIST，
// 因为名字已经被我们的 0 字节占位占住。改前树里钩子先跑、名字无人认领 ⇒ 创建成功，
// 紧接着我们的改名把它整份顶掉 ⇒ 当场红。
//
// ★ 这条钉子**不**声称钉住"第三方专门删掉我们的占位再抢"那一档 —— 那是
// claimExact 注释里写明的不可消除残余窗口（另登记，见 undoHardlink 改名处注释）。

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// strangerLands 在钩子里以"被动落子"的形态去建 path，回报那次创建的 error。
// 返回值供断言用：抢占成立时它必须是 os.ErrExist。
func strangerLands(t *testing.T, path string, fired *int) func(string) {
	t.Helper()
	return func(string) {
		*fired++
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			_, _ = f.WriteString("STRANGER-BYTES")
			_ = f.Close()
		}
		if !errors.Is(err, os.ErrExist) {
			t.Errorf("第三方在窗口里落子成功（err=%v）⇒ 那个名字没被占住，"+
				"随后的改名会把它连内容一起顶掉（M48）", err)
		}
	}
}

func setProbe(t *testing.T, h func(string)) {
	t.Helper()
	prev := beforeClaimRename
	beforeClaimRename = h
	t.Cleanup(func() { beforeClaimRename = prev })
}

func TestUndoSymlinkClaimsOriginalBeforeRenameBack(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.bin")
	if err := os.WriteFile(keep, []byte("KEEP"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := filepath.Join(dir, "dup.bin")
	if err := os.Symlink(keep, orig); err != nil {
		t.Skipf("本文件系统不支持符号链接: %v", err)
	}
	backup := orig + FddOldSuffix
	payload := []byte("ORIGINAL-BACKUP")
	if err := os.WriteFile(backup, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	it := UndoItem{Kind: "symlink", OrigPath: orig, LinkSrc: keep, Size: uint64(len(payload))}

	var fired int
	setProbe(t, strangerLands(t, orig, &fired))

	restored, err := undoSymlink(it)
	if err != nil {
		t.Fatalf("undoSymlink 失败: %v", err)
	}
	if restored != orig {
		t.Errorf("落点 = %q, want %q", restored, orig)
	}
	if fired != 1 {
		t.Fatalf("钩子触发次数 = %d, want 1（0 次说明抢占那段根本没跑到）", fired)
	}
	if got, rerr := os.ReadFile(orig); rerr != nil || string(got) != string(payload) {
		t.Errorf("原位内容 = %q（rerr=%v），want %q：备份没回位，或陌生文件占了它", got, rerr, payload)
	}
	if _, lerr := os.Lstat(backup); !os.IsNotExist(lerr) {
		t.Errorf("备份位残留: %v", lerr)
	}
}

func TestAbandonForeignBackupClaimsDupBeforeRenameBack(t *testing.T) {
	dir := t.TempDir()
	backup := filepath.Join(dir, "x"+FddOldSuffix)
	dup := filepath.Join(dir, "x.bin")
	payload := []byte("FOREIGN-BACKUP")
	if err := os.WriteFile(backup, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	var fired int
	setProbe(t, strangerLands(t, dup, &fired))

	err := abandonForeignBackup(backup, dup, filepath.Join(dir, "tmp-link"))
	if err == nil {
		t.Fatal("abandonForeignBackup 应返回错误（本次合并已放弃）")
	}
	if fired != 1 {
		t.Fatalf("钩子触发次数 = %d, want 1", fired)
	}
	// 落点契约同上：返回值/文案指的 dup 位必须装着**那份被归还的文件**，而不是陌生内容。
	if got, rerr := os.ReadFile(dup); rerr != nil || string(got) != string(payload) {
		t.Errorf("dup 位内容 = %q（rerr=%v），want %q：归还把陌生文件顶掉了", got, rerr, payload)
	}
	if _, lerr := os.Lstat(backup); !os.IsNotExist(lerr) {
		t.Errorf("备份位残留: %v", lerr)
	}
}

// 负控制：dup 位**本来就**被占（不是窗口里落的子）时，另名/保持不动那条腿必须还在。
// 没有这一条，"抢占失败就走另路"可以被悄悄改成"抢占失败也硬改名"而无人红。
func TestAbandonForeignBackupKeepsStrangerWhenDupAlreadyOccupied(t *testing.T) {
	dir := t.TempDir()
	backup := filepath.Join(dir, "y"+FddOldSuffix)
	dup := filepath.Join(dir, "y.bin")
	if err := os.WriteFile(backup, []byte("FOREIGN"), 0o644); err != nil {
		t.Fatal(err)
	}
	stranger := []byte("ALREADY-HERE")
	if err := os.WriteFile(dup, stranger, 0o644); err != nil {
		t.Fatal(err)
	}
	var fired int
	setProbe(t, func(string) { fired++ })

	if err := abandonForeignBackup(backup, dup, ""); err == nil {
		t.Fatal("应返回错误")
	}
	if got, rerr := os.ReadFile(dup); rerr != nil || string(got) != string(stranger) {
		t.Errorf("dup 位的第三方文件被改动（rerr=%v got=%q）：这一支的底线是一个字节都不碰", rerr, got)
	}
	if kept, rerr := os.ReadFile(backup); rerr != nil || string(kept) != "FOREIGN" {
		t.Errorf("备份位被清空（rerr=%v）：归还失败时 backup 才是它该被找到的位置", rerr)
	}
	if fired != 0 {
		t.Errorf("钩子触发 %d 次, want 0：dup 位非空时不该进入归还改名那一步", fired)
	}
}
