package history

// M59（04 §6.11 登记表 APP-11；设计段 §19.0-1）的"修前必红"探针。
//
// 钉的是"账本库的**每一条**连接都带会话级 PRAGMA"。改前形态：initConn 在开池时用
// db.Exec 发一遍 foreign_keys/busy_timeout/synchronous，而这三条都是**连接属性**
// —— 池一旦重建连接（空闲回收、SetConnMaxLifetime 到期、ErrBadConn），新连接读到的
// 就是 SQLite 默认值，而 foreign_keys 默认是 OFF，四处 ON DELETE CASCADE 从此静默失效。
//
// 为什么用"空闲回收"复现而不是伪装 ErrBadConn：两条触发源走的是同一个机制
// （database/sql 新建驱动连接时不会重放任何会话级语句），而前者能在本机确定性地造出来。
// 诚实边界：本用例不声称复现了真实 ErrBadConn。
//
// 两个设计细节，都是被自己的教训逼出来的：
//   - 读 pragma 必须在**持住的** *sql.Conn 上读。第一稿用 db.QueryRow 读，而
//     SetConnMaxLifetime(1ms) 会让新建连接在语句归还后立刻作废，Stats() 读到
//     OpenConnections=0，于是红在"前提自检"那一格而不是断言格（§18.7 二-6 同型）。
//   - "这次读的是新建连接"的证据是：取连接前池里 OpenConnections 已经是 0（一条都不
//     剩），因此随后拿到的那条必然是重新拨号的。DBStats 没有"累计新建数"这种单调计数
//     （Go 1.27 的 sql.DBStats 无 OpenedConnections 字段），所以只能用"池空 → 再取必新建"
//     这一步推理，取到后再用 InUse=1 复核读的是 checkout 中的那条。

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// rowQueryer 让同一段读取既能跑在 *sql.DB 上也能跑在 *sql.Conn 上。
type rowQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func readPragmas(t *testing.T, q rowQueryer) (fk, busy, synchronous int64) {
	t.Helper()
	ctx := context.Background()
	if err := q.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if err := q.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&busy); err != nil {
		t.Fatalf("PRAGMA busy_timeout: %v", err)
	}
	if err := q.QueryRowContext(ctx, `PRAGMA synchronous`).Scan(&synchronous); err != nil {
		t.Fatalf("PRAGMA synchronous: %v", err)
	}
	return fk, busy, synchronous
}

// TestEveryLedgerConnectionCarriesPragmas P-19-1。
//
// 前提自检与断言分离：先证明"第二次读确实落在一条新建连接上"（判据是本文件头部
// 记的那两步：取连接前池里 OpenConnections 已为 0 ⇒ 再取必新建，取到后用 InUse=1
// 复核读的是 checkout 中的那条），否则整条用例可能只是在重复读同一条连接，
// 红了也不说明 M59 成立。
// （M99：此处原先写"用 OpenedConnections 的增量证明"，那是被否决的旧稿说法——
// Go 1.27 的 sql.DBStats 根本没有这个字段，代码从未这么做过。）
func TestEveryLedgerConnectionCarriesPragmas(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// 首条连接（Open 里的 initConn 就是它）必须带全套 pragma。
	conn0, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatalf("取首条连接: %v", err)
	}
	fk0, busy0, sync0 := readPragmas(t, conn0)
	conn0.Close()
	if fk0 != 1 || busy0 != 5000 || sync0 != 2 {
		t.Errorf("池上首条连接就该带全套 pragma：foreign_keys=%d busy_timeout=%d synchronous=%d", fk0, busy0, sync0)
	}

	// 让那条连接作废：Go 的池会回收空闲超时/超过生命周期的连接。
	s.db.SetConnMaxIdleTime(time.Millisecond)
	s.db.SetConnMaxLifetime(time.Millisecond)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && s.db.Stats().OpenConnections != 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if n := s.db.Stats().OpenConnections; n != 0 {
		t.Fatalf("前提自检失败：旧连接没被回收（OpenConnections=%d），本次没测到新建连接那一档", n)
	}
	if st := s.db.Stats(); st.MaxLifetimeClosed+st.MaxIdleTimeClosed < 1 {
		t.Fatalf("前提自检失败：池虽空，但没有一条连接是被生命周期/空闲超时回收掉的（MaxLifetimeClosed=%d MaxIdleTimeClosed=%d），作废方式与预期不符",
			st.MaxLifetimeClosed, st.MaxIdleTimeClosed)
	}

	conn1, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatalf("取新建连接: %v", err)
	}
	defer conn1.Close()

	if st := s.db.Stats(); st.OpenConnections < 1 || st.InUse < 1 {
		t.Fatalf("前提自检失败：池空之后没拿到在用的那条连接 %+v", st)
	}
	fk1, busy1, sync1 := readPragmas(t, conn1)
	if fk1 != 1 || busy1 != 5000 || sync1 != 2 {
		t.Errorf("重建的连接没带 pragma：foreign_keys=%d want 1，busy_timeout=%d want 5000，synchronous=%d want 2(FULL)"+
			"（foreign_keys 关掉 = 四处 ON DELETE CASCADE 静默失效，子表留孤儿行）", fk1, busy1, sync1)
	}
}
