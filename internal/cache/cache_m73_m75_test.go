package cache

// M73 / M75(a)（04 §6.11 登记表；设计段 §18）的"修前必红"探针。
//
// P-18-2（下面的 TestEveryPooledConnectionCarriesPragmas）**只引用改前就存在的符号**，
// 所以它在实施前就跑到了真红（读数抄在 §18.7）。这样安排是因为登记表给的两种修法
// （DSN URI / 缩连接池）都被 §18.0 的读数否决，落地改成"每条连接建好就补 pragma"，
// 于是要有一条同时钉住"每条连接"和"池不缩"的用例；否则将来有人图省事改回
// SetMaxOpenConns(1)，今天这条红就会以另一种形状复现。
// M75(a) 那两条引用新增哨兵与接缝，修前编译不过，"修前红"由变异 M18-d/M18-e 提供。

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/fsid"
)

// pragmaPairOn 读一条连接上的会话级 PRAGMA。busy_timeout/synchronous 都是
// **连接属性**（不像 journal_mode 落在文件头），所以取到的值就是"这条连接有没有
// 被配置过"的真读数。
func pragmaPairOn(t *testing.T, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) (busy, synchronous int) {
	t.Helper()
	if err := q.QueryRowContext(context.Background(), `PRAGMA busy_timeout`).Scan(&busy); err != nil {
		t.Fatalf("PRAGMA busy_timeout: %v", err)
	}
	if err := q.QueryRowContext(context.Background(), `PRAGMA synchronous`).Scan(&synchronous); err != nil {
		t.Fatalf("PRAGMA synchronous: %v", err)
	}
	return busy, synchronous
}

// P-18-2（M73）：池里**每一条**连接都必须带 busy_timeout=5000、synchronous=1(NORMAL)。
// 预测的修前红法：db.Exec 只配置它自己那条连接，额外连接读到 SQLite 默认
// busy_timeout=0、synchronous=2(FULL)。
func TestEveryPooledConnectionCarriesPragmas(t *testing.T) {
	c := openTest(t)
	// 显式放到 3：本用例同时钉住"不靠缩连接池蒙混"（§18.0 取证 #4：缩到 1 让
	// 8 worker × 3000 次点查从 139.1ms 涨到 289.8ms，代价实测 2.08 倍）。
	c.db.SetMaxOpenConns(3)
	if n := c.db.Stats().MaxOpenConnections; n != 3 {
		t.Fatalf("本用例前提：连接池不得被缩成 1（实测 MaxOpenConnections=%d）", n)
	}
	b1, s1 := pragmaPairOn(t, c.db)
	if b1 != 5000 || s1 != 1 {
		t.Errorf("池上首条连接也应带 pragma：busy_timeout=%d synchronous=%d", b1, s1)
	}

	ctx := context.Background()
	// 各持有一条连接，逼 sql 包真的再开第二条、第三条（连接按需新建，
	// 不占住前一条就只会重复用同一条，测不到"新连接"那一档）。
	hold1, err := c.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer hold1.Close()
	hold2, err := c.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer hold2.Close()

	for i, cn := range []*sql.Conn{hold1, hold2} {
		b, s := pragmaPairOn(t, cn)
		if b != 5000 || s != 1 {
			t.Errorf("第 %d 条额外连接的 pragma 没生效：busy_timeout=%d want 5000，synchronous=%d want 1(NORMAL)",
				i+2, b, s)
		}
	}
}

// 前提自检（M75(a)）：淘汰那步在真实 SQLite 下确实会失败。
// 这条不引用任何新增符号，也不需要接缝——直接以"计数已超限"的状态调 evictLocked，
// 让它发出真 DELETE，被库上的 RAISE 触发器挡下来。
// 为什么要单独钉：下面的接缝用例把"淘汰失败"做成了返回一个假错误，
// 若淘汰这一步根本不可能失败，那用例就是在为一个不存在的分支造句。
func TestEvictDeleteCanReallyFail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.db")
	c, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Store([]Entry{{Path: "/keep", Size: 1, MtimeNs: 1, Head: 7, Tail: 7}}); err != nil {
		t.Fatal(err)
	}
	db2, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db2.Exec(`CREATE TRIGGER zz_no_delete BEFORE DELETE ON hash_cache
		BEGIN SELECT RAISE(ABORT, '探针：禁止淘汰'); END`); err != nil {
		db2.Close()
		t.Fatalf("装触发器失败: %v", err)
	}
	db2.Close()

	c.mu.Lock()
	c.cnt = MaxEntries + 1
	c.cntValid = true // 真超限：跳过 COUNT，直接走 DELETE
	err = c.evictLocked()
	c.mu.Unlock()
	if err == nil {
		t.Fatal("DELETE 触发器没生效：淘汰根本不会失败，后面的接缝用例就没有对应现实")
	}
	if !strings.Contains(err.Error(), "禁止淘汰") {
		t.Errorf("淘汰失败应是 SQLite 原文（含触发器消息）：%q", err.Error())
	}
}

// P-18-4（M75(a)）：Commit 已成功、只有淘汰失败时，Store 的错误必须是可判定的
// 哨兵 ErrEvictFailed，并且**自带**"已写回"这件事——上游据此才说得出一句真话。
//
// ★ 这条不是"修前必红"：设计段 18.3 预测可以用"伪造超限计数 + 触发器"从 Store
// 打进去，实测打不通——Store 在 Commit 之后会把 cntValid 置回 false（C5 的既有语义），
// evictLocked 于是先重算 COUNT 得 2 条、判定未超限、连 DELETE 都不发，返回 nil。
// 于是本用例改走 evictFn 接缝，"修前红"由变异 M18-d/M18-e 提供（见 §18.7）。
func TestStoreEvictFailureSaysWriteBackSucceeded(t *testing.T) {
	c := openTest(t)
	if err := c.Store([]Entry{{Path: "/keep", Size: 1, MtimeNs: 1, Head: 7, Tail: 7}}); err != nil {
		t.Fatal(err)
	}
	orig := evictFn
	evictFn = func(*Cache) error { return errors.New("探针：淘汰语句失败") }
	t.Cleanup(func() { evictFn = orig })

	err := c.Store([]Entry{{Path: "/new", Size: 2, MtimeNs: 2, Head: 8, Tail: 8}})
	if err == nil {
		t.Fatal("淘汰失败却返回了 nil，本用例前提不成立")
	}
	if !errors.Is(err, ErrEvictFailed) {
		t.Errorf("淘汰失败必须是可判定的哨兵（上游据此分岔文案）：%v", err)
	}
	if !strings.Contains(err.Error(), "已写回") {
		t.Errorf("错误里必须自带『已写回』这个事实，否则又是一句会被读成『写回失败』的话：%q", err.Error())
	}
	if !strings.Contains(err.Error(), "探针：淘汰语句失败") {
		t.Errorf("底层原因不能丢：%q", err.Error())
	}
	// 事实核对：/new 确实进了库——"已写回"不能是空话。
	if _, hit, _ := c.Lookup("/new", 2, 2, fsid.ID{}); !hit {
		t.Error("Commit 已成功，/new 应查得到，否则『已写回』这句又成了假话")
	}
}

// P-18-4b（M75(a) 的反面）：真正的写回失败不得披上"已写回"的外衣。
func TestStoreWriteBackFailureIsNotReportedAsEvictFailure(t *testing.T) {
	c := openTest(t)
	if err := c.Store([]Entry{{Path: "/a", Size: 1, MtimeNs: 1, Head: 5, Tail: 5}}); err != nil {
		t.Fatal(err)
	}
	// 关库后再写：失败发生在 Begin/Exec，属"没写进去"那一档。
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	err := c.Store([]Entry{{Path: "/b", Size: 2, MtimeNs: 2, Head: 6, Tail: 6}})
	if err == nil {
		t.Fatal("库已关闭，写回应失败")
	}
	if errors.Is(err, ErrEvictFailed) {
		t.Errorf("写回失败被误分类成淘汰失败（等于把『没写进去』说成『已写回』）：%v", err)
	}
}
