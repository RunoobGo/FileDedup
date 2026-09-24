package dbfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 判定必须「只认强指征」：真损坏（本机 SQLite 实测原文）走隔离，
// 暂时性/环境故障必须落回 false，否则一次误判就清库。
func TestIsCorruption(t *testing.T) {
	corrupt := []string{
		"初始化失败: file is not a database (26)",
		"初始化失败: database disk image is malformed (11)",
		"database page 3 is never used",
		"SQLITE_CORRUPT",
		"sqlite3: sqlite_notadb",
		"RDONLY: repeated read error: is not a database",
	}
	// M146（第 3 轮 §24.4 DBF-1）：下界本身也是判据。这两张表都是硬编码清单，
	// 表被删空时本条等于没跑 —— 循环 0 次、一条断言都不执行，整条测试照样绿。
	// 先例同法：app_undo_platform_probe_test.go 的 `checked != 4`、
	// sqlconn_test.go 与 app_pathnorm_gate_test.go 的 `scanned < 50`。
	if len(corrupt) < 6 {
		t.Fatalf("corrupt 表条目数 = %d，下界 6：判据清单被删空 ⇒ 本条等于没跑（M146）", len(corrupt))
	}
	for _, s := range corrupt {
		if !IsCorruption(errors.New(s)) {
			t.Errorf("应判为损坏: %q", s)
		}
	}
	// 这些都是「改好环境就能恢复」的故障：判成损坏等于把库搬走
	transient := []string{
		"unable to open database file (14)",
		"database is locked (5)",
		"SQLITE_BUSY: database is locked",
		"disk I/O error",
		"attempt to write a readonly database (8)",
		"no such table: hash_cache",
		"too many SQL commands",
		"",
	}
	// M146：同上，transient 表的下界。误判方向一旦失守就是清库，
	// 这条不允许靠「表空了 ⇒ 循环 0 次 ⇒ 通过」蒙过去。
	if len(transient) < 8 {
		t.Fatalf("transient 表条目数 = %d，下界 8：判据清单被删空 ⇒ 本条等于没跑（M146）", len(transient))
	}
	for _, s := range transient {
		if IsCorruption(errors.New(s)) {
			t.Errorf("误判为损坏（会导致隔离/重建）: %q", s)
		}
	}
	if IsCorruption(nil) {
		t.Error("nil 不得判为损坏")
	}
	// 包装层（%w 链）也要认得出
	if !IsCorruption(fmt.Errorf("外层: %w", errors.New("file is not a database (26)"))) {
		t.Error("包装后的损坏错误未被识别")
	}
}

func TestExists(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "a.db")
	if Exists(p) {
		t.Error("不存在的文件报 Exists")
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !Exists(p) {
		t.Error("已存在的文件报 !Exists")
	}
	// 目录本身也报存在：调用方据此区分「文件不在」与「文件读不动」
	if !Exists(d) {
		t.Error("目录应报 Exists")
	}
}

// 隔离只改名不删除：误判也要留得回原始影像。
func TestQuarantineRenamesAndKeepsBytes(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "history.db")
	payload := []byte("irreplaceable undo ledger image")
	if err := os.WriteFile(p, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p+"-wal", []byte("wal"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p+"-shm", []byte("shm"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Quarantine(p)
	if err != nil {
		t.Fatal(err)
	}
	if Exists(p) || Exists(p+"-wal") || Exists(p+"-shm") {
		t.Fatalf("隔离后原路径仍存在: %s", p)
	}
	if !strings.HasPrefix(filepath.Base(got), "history.db.broken-") {
		t.Errorf("隔离名不合约定: %s", filepath.Base(got))
	}
	back, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != string(payload) {
		t.Error("隔离过程改动了影像内容")
	}
	for _, s := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(got + s); err != nil {
			t.Errorf("侧文件未跟随隔离: %s%v", got, err)
		}
	}
}

// 主文件改名失败必须报出去（调用方据此放弃重建，而不是转而删文件）。
func TestQuarantineFailsOnMissingMain(t *testing.T) {
	p := filepath.Join(t.TempDir(), "gone.db")
	if _, err := Quarantine(p); err == nil {
		t.Fatal("主文件不存在时应返回错误")
	}
}

// hasBrokenResidue 报告目录下是否残留任何 .broken-* 隔离名（含主/侧）。
func hasBrokenResidue(t *testing.T, dir string) (bool, []string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if strings.Contains(e.Name(), ".broken-") {
			names = append(names, e.Name())
		}
	}
	return len(names) > 0, names
}

// P-213-a（修前真红 · M213）：-wal 改名成功、-shm 改名失败时，必须
// ① 返回点名 -shm 的错误（改前 `_ =` 吞掉 ⇒ err==nil ⇒ 首格红）；
// ② 把已隔离的主文件与已改名的 -wal 全部回滚到原路径（改前不滚 ⇒ 回原位格红）；
// ③ 不留任何 .broken-* 残留。
func TestQuarantineSideFailureRollsBackAndErrors(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "cache.db")
	for _, f := range []string{p, p + "-wal", p + "-shm"} {
		if err := os.WriteFile(f, []byte("img"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	origRename := renameFile
	t.Cleanup(func() { renameFile = origRename })
	// 注入：仅 -shm 的前向改名失败（src 恰为原路径 -shm）。回滚调用 src 是隔离名，不受影响。
	renameFile = func(src, dst string) error {
		if src == p+"-shm" {
			return errors.New("injected: EPERM on -shm")
		}
		return origRename(src, dst)
	}

	_, err := Quarantine(p)
	if err == nil {
		t.Fatal("侧文件改名失败必须返回错误（改前静默吞 ⇒ 此格红）")
	}
	if !strings.Contains(err.Error(), "-shm") {
		t.Errorf("错误未点名失败的侧文件 -shm：%v", err)
	}
	// 主影像与已改名的 -wal 都应回到原路径。
	if !Exists(p) {
		t.Error("主影像未回滚到原路径")
	}
	if !Exists(p + "-wal") {
		t.Error("-wal 未随主影像一起回滚到原路径")
	}
	// -shm 从未被挪走，理应仍在原地。
	if !Exists(p + "-shm") {
		t.Error("-shm 应仍在原路径（其改名被注入为失败）")
	}
	if residue, names := hasBrokenResidue(t, d); residue {
		t.Errorf("回滚后仍残留 .broken-* 隔离名：%v", names)
	}
}

// P-213-c（回滚本身失败 ⇒ 绝不静默）：-wal 改名失败触发回滚，
// 而主影像回滚也被注入为失败 ⇒ 必须返回点名"勿在原路径重建"的错误，
// 且主影像确实留在隔离名处（不假装回到原位）。
func TestQuarantineRollbackFailureIsLoud(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "ledger.db")
	if err := os.WriteFile(p, []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p+"-wal", []byte("wal"), 0o644); err != nil {
		t.Fatal(err)
	}
	origRename := renameFile
	t.Cleanup(func() { renameFile = origRename })
	renameFile = func(src, dst string) error {
		if src == p+"-wal" {
			return errors.New("injected: side -wal rename fails") // 触发回滚
		}
		if dst == p {
			return errors.New("injected: main rollback fails") // 主影像挪不回原位
		}
		return origRename(src, dst)
	}

	_, err := Quarantine(p)
	if err == nil {
		t.Fatal("回滚失败仍须返回错误")
	}
	if !strings.Contains(err.Error(), "勿在原路径重建") {
		t.Errorf("回滚失败的错误必须点名'勿在原路径重建'：%v", err)
	}
	// 主影像回滚失败 ⇒ 它留在隔离名处（不在原路径）。
	if Exists(p) {
		t.Error("主影像回滚被注入为失败，不应出现在原路径")
	}
	if residue, _ := hasBrokenResidue(t, d); !residue {
		t.Error("主影像回滚失败 ⇒ 应能在隔离名处看到 .broken-*（证明它确被挪走且未假装回来）")
	}
}

// P-213-b（负控制）：无侧文件时行为与改前一致——err==nil、主文件隔离、原名消失。
func TestQuarantineNoSideFilesUnchanged(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "solo.db")
	if err := os.WriteFile(p, []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Quarantine(p)
	if err != nil {
		t.Fatalf("无侧文件时不应报错：%v", err)
	}
	if Exists(p) {
		t.Error("主文件应已隔离，原路径不该存在")
	}
	if !strings.HasPrefix(filepath.Base(got), "solo.db.broken-") {
		t.Errorf("隔离名不合约定：%s", filepath.Base(got))
	}
}
