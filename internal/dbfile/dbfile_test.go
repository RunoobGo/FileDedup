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
