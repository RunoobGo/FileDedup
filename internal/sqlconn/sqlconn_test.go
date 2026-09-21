package sqlconn

// internal/sqlconn 的机制测试（设计段 §19.3 P-19-1b）。
//
// 全部用**替身驱动**，不碰 SQLite：本包要钉的是"连接出生即重放会话级语句"这条
// 与库无关的机制，用真 SQLite 反而会把"modernc 的 PRAGMA 恰好支持 Exec"混进来。
// 真实库上的端到端证据在两个调用方各自的下家里：
//   - internal/cache：TestNewCacheConnectionsCarrySessionPragmas（§18 P-18-2）
//   - internal/history：TestEveryLedgerConnectionCarriesPragmas（§19 P-19-1）
//
// 收归的等价性锁分两格：机制行为逐条对得上改前 cache 里的那份实现（下面 1~4），
// 加上"全仓只有本包持有这份机制"（下面第 5 条门禁）。

import (
	"context"
	"database/sql/driver"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------- 替身驱动 ----------

// log 是三种连接共用的心脏：记语句、记是否被关掉、可按名字注入失败。
type log struct {
	stmts  []string
	closed bool
	failOn string // 匹配则该语句报错
	base   driver.Conn
}

func (l *log) run(q string) error {
	l.stmts = append(l.stmts, q)
	if l.failOn != "" && strings.Contains(q, l.failOn) {
		return errors.New("模拟：该语句在连接上执行失败")
	}
	return nil
}

func (l *log) Close() error { l.closed = true; return nil }

// execConn 实现 driver.ExecerContext（modernc 的驱动就是这个形态）。
type execConn struct{ log }

func (c *execConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("替身不该走 Prepare")
}
func (c *execConn) Begin() (driver.Tx, error) { return nil, errors.New("替身不支持事务") }
func (c *execConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	return nil, c.run(query)
}

// prepConn **不**实现 ExecerContext，用来验退回 Prepare 的那条腿还在。
type prepConn struct {
	log
	prepared []string
	stmtFail string
}

func (c *prepConn) Prepare(query string) (driver.Stmt, error) {
	c.prepared = append(c.prepared, query)
	return &prepStmt{owner: c}, nil
}
func (c *prepConn) Begin() (driver.Tx, error) { return nil, errors.New("替身不支持事务") }

type prepStmt struct {
	owner  *prepConn
	closed bool
}

func (s *prepStmt) Close() error  { s.closed = true; return nil }
func (s *prepStmt) NumInput() int { return 0 }
func (s *prepStmt) Exec([]driver.Value) (driver.Result, error) {
	// 按语句记账：替身连接自己的 log 才是事实源，不能依赖"最后一条 prepared"
	// （失败时那条腿可能压根没走到这里）。
	last := s.owner.prepared[len(s.owner.prepared)-1]
	return nil, s.owner.run("prepared:" + last)
}
func (s *prepStmt) Query([]driver.Value) (driver.Rows, error) {
	return nil, errors.New("替身不支持查询")
}

// fakeConnector 每次 Connect 造一条新连接并登记，用于数"新建了几条"。
type fakeConnector struct {
	drv     driver.Driver
	created []driver.Conn
	connErr error
	newConn func() driver.Conn
}

func (c *fakeConnector) Connect(context.Context) (driver.Conn, error) {
	if c.connErr != nil {
		return nil, c.connErr
	}
	conn := c.newConn()
	c.created = append(c.created, conn)
	return conn, nil
}
func (c *fakeConnector) Driver() driver.Driver { return c.drv }

type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) { return nil, errors.New("替身不实现 Open") }

// ---------- 机制逐条 ----------

// TestReplaysOnEveryNewConnection 核心判据：同一个包装器上**每次** Connect 都要重放，
// 顺序与清单一致。改前的缺口正是"只有开池那条连接有"，所以第二条是必钉的一格。
func TestReplaysOnEveryNewConnection(t *testing.T) {
	pragmas := []string{"PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000"}
	base := &fakeConnector{drv: fakeDriver{}, newConn: func() driver.Conn {
		return &execConn{log: log{failOn: ""}}
	}}
	pc := WithPragmas(base, pragmas)

	for round := 1; round <= 2; round++ {
		conn, err := pc.Connect(context.Background())
		if err != nil {
			t.Fatalf("第 %d 条连接: %v", round, err)
		}
		ec, ok := conn.(*execConn)
		if !ok {
			t.Fatalf("第 %d 条连接类型 %T", round, conn)
		}
		if strings.Join(ec.stmts, " ") != strings.Join(pragmas, " ") {
			t.Errorf("第 %d 条连接重放 = %v, want %v", round, ec.stmts, pragmas)
		}
	}
	if len(base.created) != 2 {
		t.Fatalf("替身应被建 2 次，实测 %d", len(base.created))
	}
}

// TestFailClosedOnPragmaError 任一条 pragma 失败 ⇒ 连接不得交出，且必须被关闭。
// 半套 pragma 的连接交出去 = 换个触发条件继续"foreign_keys 静默 OFF"。
func TestFailClosedOnPragmaError(t *testing.T) {
	base := &fakeConnector{drv: fakeDriver{}, newConn: func() driver.Conn {
		return &execConn{log: log{failOn: "busy_timeout"}}
	}}
	conn, err := WithPragmas(base, []string{"PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000"}).Connect(context.Background())
	if err == nil {
		t.Fatal("pragma 执行失败却把连接交了出去")
	}
	if conn != nil {
		t.Errorf("失败路径必须返回 nil 连接，实测 %T", conn)
	}
	if len(base.created) != 1 {
		t.Fatalf("替身应只被建 1 次，实测 %d", len(base.created))
	}
	if !base.created[0].(*execConn).closed {
		t.Error("失败路径没关掉那条半成品连接（泄漏到底层驱动）")
	}
	if !strings.Contains(err.Error(), "PRAGMA busy_timeout=5000") {
		t.Errorf("错误必须点名是哪条 pragma: %v", err)
	}
}

// TestFallsBackToPrepare 驱动不实现 ExecerContext 时仍要重放（只支持旧接口的驱动
// 否则整库打不开）。
func TestFallsBackToPrepare(t *testing.T) {
	base := &fakeConnector{drv: fakeDriver{}, newConn: func() driver.Conn {
		return &prepConn{log: log{}}
	}}
	conn, err := WithPragmas(base, []string{"PRAGMA foreign_keys=ON"}).Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	pc := conn.(*prepConn)
	if strings.Join(pc.prepared, ",") != "PRAGMA foreign_keys=ON" {
		t.Errorf("退回 Prepare 的那条腿没走通: prepared=%v stmts=%v", pc.prepared, pc.stmts)
	}
}

// TestPrepareFallbackFailClosed Prepare 腿同样必须失败即拦。
func TestPrepareFallbackFailClosed(t *testing.T) {
	base := &fakeConnector{drv: fakeDriver{}, newConn: func() driver.Conn {
		return &prepConn{log: log{failOn: "foreign_keys"}}
	}}
	if _, err := WithPragmas(base, []string{"PRAGMA foreign_keys=ON"}).Connect(context.Background()); err == nil {
		t.Fatal("Prepare 腿执行失败仍把连接交了出去")
	}
}

// TestPassthroughAndDriverDelegation 空清单 = 直通（收归等价性：不改任何语义），
// 且 Driver() 必须原样委托给底层（database/sql 用它做 dsn 归一与类型判定）。
func TestPassthroughAndDriverDelegation(t *testing.T) {
	base := &fakeConnector{drv: fakeDriver{}, newConn: func() driver.Conn {
		return &execConn{log: log{}}
	}}
	pc := WithPragmas(base, nil)
	conn, err := pc.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := conn.(*execConn).stmts; len(got) != 0 {
		t.Errorf("空清单不该发任何语句: %v", got)
	}
	if pc.Driver() != base.drv {
		t.Error("Driver() 未委托给底层连接器")
	}
	if _, err := WithPragmas(&fakeConnector{drv: fakeDriver{}, connErr: errors.New("底层拨号失败"),
		newConn: func() driver.Conn { return &execConn{} }}, []string{"PRAGMA x"}).Connect(context.Background()); err == nil {
		t.Error("底层连接失败必须原样上抛")
	}
}

// ---------- 收归门禁 ----------

// pragmaMechanismAllowlist 是唯一被允许持有"逐连接重放会话级语句"机制的文件。
var pragmaMechanismAllowlist = map[string]bool{
	filepath.Join("internal", "sqlconn", "sqlconn.go"): true,
}

// TestSessionPragmaMechanismHasSingleImplementation 扫全仓非测试 Go 文件，
// 断言 driver.Connector 的自定义实现只出现在白名单里。
//
// 为什么值得一条门禁而不是"改完就算"：M73（cache）与 M59（history）正是同一条
// 判据各自长出来的两份，而两份的**结论不同**（cache 靠包装生效、history 那句注释
// 以为靠缩池生效并被实测证伪）。不收归并钉住，第三个用 SQLite 的库会再长一份。
//
// 判据只覆盖非测试文件：测试里的替身是**独立基准**，不是重复的生产判据。
func TestSessionPragmaMechanismHasSingleImplementation(t *testing.T) {
	root := filepath.Join("..", "..")
	var offenders []string
	var scanned int
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if p != root && (name[0] == '.' || name == "node_modules" || name == "dist") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		scanned++
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			rel = p
		}
		if pragmaMechanismAllowlist[rel] {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		text := string(b)
		// 两条特征：自己实现 Connect(ctx) 返回 driver.Conn，或调用 sql.Open 之外的
		// 包装手法在连接上跑会话级 PRAGMA。
		if strings.Contains(text, "driver.Connector") ||
			strings.Contains(text, "func (") && strings.Contains(text, ") Connect(ctx context.Context) (driver.Conn, error)") {
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned < 50 {
		t.Fatalf("只扫到 %d 个非测试 Go 文件，遍历本身失效（这条门禁的下界也是判据）", scanned)
	}
	if len(offenders) > 0 {
		t.Errorf("会话级 PRAGMA 的逐连接机制出现第二份实现（I5），应改调 internal/sqlconn：%v", offenders)
	}
	t.Logf("M59 方法面：扫描 %d 个非测试文件 / 违规 %d 处", scanned, len(offenders))
}
