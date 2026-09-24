package history

// 2026-09-18 审查 C4 回归：回撤账本的「损坏自愈」必须可判定。
// 真损坏 → 改名隔离后重建；BUSY/只读/满盘等暂时性故障 → 只报错，账本原样保留。

import (
	"os"
	"path/filepath"
	"testing"
)

func seedLedger(t *testing.T, p string) {
	t.Helper()
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.SaveScan(mkScanCfg(), mkGroups(1), nil)
	if err != nil {
		t.Fatal(err)
	}
	jid, err := s.BeginOp("trash", "", id, true, []OpItemPlan{
		{OrigPath: "/root/a/0.bin", Size: 1000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishItem(jid, "/root/a/0.bin", "/Trash/a.bin", "", StateDone, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.FinalizeOp(jid, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCorruptLedgerQuarantinedNotDeleted(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "history.db")
	junk := []byte("garbage where an undo ledger used to live")
	if err := os.WriteFile(p, junk, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(p)
	if err != nil {
		t.Fatalf("确证损坏应隔离重建: %v", err)
	}
	s.Close()
	matches, _ := filepath.Glob(p + ".broken-*")
	if len(matches) != 1 {
		t.Fatalf("隔离文件 = %v, want 1 个", matches)
	}
	payload, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != string(junk) {
		t.Errorf("隔离后的内容不符: %q", payload)
	}
}

// blockWalSidecar 用「-wal 位置被目录占住」制造暂时性打开失败：
// 库文件完好、目录可写、权限正常，只是这一刻建不起 WAL 侧文件。
// 真实成因可能是同步盘/备份工具留下同名目录，也可能是他进程正在写库。
// 关键性质：修复前的自愈 os.Remove(主库) 在此**会成功**，所以这条用例能
// 真正区分「修复前把账本删了重建」与「修复后原样报错」。
func blockWalSidecar(t *testing.T, p string) {
	t.Helper()
	if err := os.Mkdir(p+"-wal", 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(p + "-wal"); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	})
}

// 暂时性故障下账本必须活着：丢了可回撤账本 = 用户永久失去找回文件的线索。
func TestTransientFailureKeepsLedger(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "history.db")
	seedLedger(t, p)
	before, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}

	blockWalSidecar(t, p)
	if _, err := Open(p); err == nil {
		t.Fatal("库暂时打不开时 Open 应失败")
	} else {
		t.Logf("暂时性故障按预期上抛: %v", err)
	}
	after, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("暂时性故障不得改动账本文件")
	}
	if broken, _ := filepath.Glob(p + ".broken-*"); len(broken) != 0 {
		t.Errorf("暂时性故障不得隔离/搬走账本: %v", broken)
	}
	if err := os.Remove(p + "-wal"); err != nil {
		t.Fatal(err)
	}

	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ops, err := s.ListOps()
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || !ops[0].Undoable || ops[0].Done != 1 {
		t.Fatalf("故障后账本条目丢失或走样: %+v", ops)
	}
}
