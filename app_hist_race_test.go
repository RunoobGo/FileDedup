package main

// M9（2026-09-21 全仓审计 §五 9）：`a.hist` 是受 `a.mu` 保护的字段，
// 读它必须在锁内。
//
// 现场：`shutdown` 在锁内 `a.hist.Close(); a.hist = nil`（app.go:334），而全仓有
// 九处绑定层入口与扫描 goroutine 直接 `if a.hist == nil { ... }; a.hist.X()`——
// 字段在锁外读两次。`-race` 下这就是一次数据竞争；即便不竞争，第二次读也可能读到
// 刚被置空的值而 panic 在已关闭的句柄上。
//
// 本文件两件事：
//   ① 竞争探针：读侧走真实 RPC 入口，写侧复刻 shutdown 的锁内置空。改前必红
//      （race 报告），改后绿（快照只读一次、且在锁内）。
//   ② 结构性门禁：把"字段只能在白名单函数里直接读、且只以 `hs := a.hist` 的形式
//      读一次"钉成测试。新增 RPC 若又写 `a.hist.Method()` 即红——这类竞争从返回值
//      上永远观测不到，只能靠门禁拦。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"sync"
	"testing"
)

// TestHistFieldReadsRaceWithShutdown 并发读九个 RPC 入口 + 复刻 shutdown 的写侧。
func TestHistFieldReadsRaceWithShutdown(t *testing.T) {
	a, _ := newHistApp(t)
	hs := a.hist // 写侧每轮放回同一个句柄：本探针不关库，只测字段访问的同步性

	readers := []func(){
		func() { _, _ = a.ListScanHistory() },
		func() { _, _ = a.LoadScanHistory(1) },
		func() { _ = a.DeleteScanHistory(1) },
		func() { _ = a.ClearScanHistory() },
		func() { _, _ = a.ListOpRecords() },
		func() { _, _ = a.GetOpRecord(1) },
		func() { _ = a.ClearOpRecords() },
		func() { _ = a.ClearKeepDecisions() },
	}

	stop := make(chan struct{})
	var rwg sync.WaitGroup
	for i := 0; i < 4; i++ {
		rwg.Add(1)
		go func() {
			defer rwg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				for _, r := range readers {
					r()
				}
			}
		}()
	}
	// 写侧：与 shutdown 逐字同形的两段（置 nil 前已 Close 的语义这里不需要——
	// 竞争发生在"读字段"这一步，与句柄是否关闭无关）。
	for i := 0; i < 300; i++ {
		a.mu.Lock()
		a.hist = nil
		a.mu.Unlock()
		a.mu.Lock()
		a.hist = hs
		a.mu.Unlock()
	}
	close(stop)
	rwg.Wait()
}

// histFieldDirectReadFuncs 允许直接读写 a.hist 的函数（其余一律走 histSnapshot）。
//
// startup / shutdown 是生命周期的两端（赋值、锁内 Close+置空）；histSnapshot 是唯一
// 出口。这三处怎么写都行。
var histFieldDirectReadFuncs = map[string]bool{
	"startup": true, "shutdown": true, "histSnapshot": true,
}

// histSnapshotOnlyFuncs 允许读字段、但**只许**以 `hs := a.hist` 形式读一次的函数：
// 它们在自己的大临界区内取一次快照后全程用 hs。不能再调 histSnapshot——会与已持有的
// a.mu 自锁死，所以这里给它们留口，同时钉死写法。
var histSnapshotOnlyFuncs = map[string]bool{
	"ExecuteOperation": true, "UndoOperation": true, "UndoOperationItem": true,
}

// TestHistFieldNotReadOutsideLock 用 AST 钉住：app.go 里每个 a.hist 引用都在白名单
// 函数内，且"只许快照"的那几个函数确实是快照式写法。
func TestHistFieldNotReadOutsideLock(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "app.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	// 快照赋值：`hs := a.hist` / `hs = a.hist` 的 RHS 节点集合。
	snapshotRHS := map[ast.Expr]bool{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			if as, ok := n.(*ast.AssignStmt); ok {
				for _, rhs := range as.Rhs {
					if isHistSel(rhs) {
						snapshotRHS[rhs] = true
					}
				}
			}
			return true
		})
	}
	var offenders []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		name := fn.Name.Name
		ast.Inspect(fn, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || !isHistSel(sel) {
				return true
			}
			switch {
			case histFieldDirectReadFuncs[name]:
			case histSnapshotOnlyFuncs[name] && snapshotRHS[sel]:
			default:
				offenders = append(offenders, name+": "+fset.Position(sel.Pos()).String())
			}
			return true
		})
	}
	if len(offenders) > 0 {
		t.Fatalf("a.hist 被锁外读取（M9 回归）；请改走 a.histSnapshot()：%s",
			strings.Join(offenders, "\n  "))
	}
}

func isHistSel(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "a" && sel.Sel.Name == "hist"
}
