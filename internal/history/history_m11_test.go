package history

// M11（2026-09-21 全仓审计 §五 11）：账本库的持久性参数。
// 本条只钉配置、钉不了行为：NORMAL 与 FULL 的差别只在掉电/内核崩溃那一档显现，
// 而它要防的那次丢账（BeginOp 已写、删除已生效、账本里却没有这条）在测试环境里
// 无法复现——软件层的任何"崩溃恢复"测试走的都是 WAL 回放路径，那条路两种参数
// 都能过。所以这里与 §6.8 的 AST 清点同属一类：把不可观测的口径钉成断言。

import (
	"testing"
)

// SQLite synchronous 取值：0=OFF 1=NORMAL 2=FULL 3=EXTRA。
const (
	synchronousNormal = 1
	synchronousFull   = 2
)

func TestLedgerRequiresSynchronousFull(t *testing.T) {
	s := testDB(t)

	var mode string
	if err := s.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %s，期望 wal（进程崩溃靠 WAL 回放，与下面的 FULL 各管一档）", mode)
	}

	var sync int
	if err := s.db.QueryRow(`PRAGMA synchronous`).Scan(&sync); err != nil {
		t.Fatal(err)
	}
	if sync != synchronousFull {
		t.Fatalf("账本库 synchronous = %d（%d=NORMAL），期望 %d=FULL：NORMAL 下 commit 不 fsync，"+
			"掉电时最近的 BeginOp/FinishItem 随 WAL 一起丢，而删除已经生效——账本丢了就是永久失去撤销依据",
			sync, synchronousNormal, synchronousFull)
	}
}
