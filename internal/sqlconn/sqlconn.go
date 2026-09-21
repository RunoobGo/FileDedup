// Package sqlconn 收归"每条连接建好即重放会话级 PRAGMA"这一条判据（I5）。
//
// 会话级 PRAGMA（foreign_keys / busy_timeout / synchronous）是**连接属性**，不是
// 文件属性：db.Exec 一遍只对当时那条连接生效，池把那条连接回收掉之后，新建的连接
// 读到的是 SQLite 默认值，而 database/sql 不会重放任何会话级语句。
// 这条读数不是推断，是实测（04 §6.11 M59 / 设计段 §19.0-1）：
//
//	池上首条连接：foreign_keys=1 busy_timeout=5000 synchronous=2
//	空闲回收后重建：foreign_keys=0 busy_timeout=0   synchronous=2
//
// 唯一可靠的做法是把 PRAGMA 挂在连接的**出生**上，即包一层 driver.Connector。
// 缓存库（internal/cache，M73）与账本库（internal/history，M59）各自实现一遍就是
// 同一条判据的两份代码 —— 本包把它收成一份。
//
// 两条刻意不做的修法及其理由（照抄 cache 的 M73 取证，设计段 §18.0 取证 #3/#4）：
//   - DSN URI 形（file:...?_pragma=）：含 # 的路径会被当作 URI fragment，Windows
//     路径的 C: 会被吃成 authority，实测库建到了别处；
//   - SetMaxOpenConns(1)：它只是让"很少建新连接"，不消除缺口，且有实测代价
//     （缓存库 8 worker × 3000 次点查 139.1ms → 289.8ms）。
package sqlconn

import (
	"context"
	"database/sql/driver"
	"fmt"
)

// WithPragmas 返回一个新的 Connector：每次底层连接器建成一条新连接，
// 都在该连接上按顺序重放 pragmas，全部成功才把连接交给 database/sql。
//
// pragmas 里放的是**会话级**语句（文件级持久属性如 journal_mode=WAL 不该放这里，
// 建库时 Exec 一次即可，新连接天然读到 wal）。
//
// 任一条失败即关闭连接并返回错误 —— 半套 pragma 的连接交出去，等于把
// "foreign_keys 静默 OFF"这类事故换个触发条件而已（fail-closed）。
func WithPragmas(base driver.Connector, pragmas []string) driver.Connector {
	return pragmaConnector{base: base, pragmas: pragmas}
}

type pragmaConnector struct {
	base    driver.Connector
	pragmas []string
}

func (pc pragmaConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := pc.base.Connect(ctx)
	if err != nil {
		return nil, err
	}
	if err := apply(conn, ctx, pc.pragmas); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func (pc pragmaConnector) Driver() driver.Driver { return pc.base.Driver() }

// apply 在单条连接上重放 pragma。优先走 ExecerContext；驱动不实现时才退回
// Prepare/Exec（退回路径必须存在，否则换个只支持旧接口的驱动就整库打不开）。
func apply(conn driver.Conn, ctx context.Context, pragmas []string) error {
	ex, canExec := conn.(driver.ExecerContext)
	for _, p := range pragmas {
		if canExec {
			if _, err := ex.ExecContext(ctx, p, nil); err != nil {
				return fmt.Errorf("%s: %w", p, err)
			}
			continue
		}
		stmt, err := conn.Prepare(p)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		_, err = stmt.Exec(nil)
		_ = stmt.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
	}
	return nil
}
