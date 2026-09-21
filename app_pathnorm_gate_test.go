package main

// M64（I5-1）门禁：路径分隔符归一在**生产代码**里只允许一处实现。
//
// 为什么需要这条门禁而不是"改完就算"：`strings.ReplaceAll(x, sep, "/")` 这个判断
// 在本仓曾长到四份（scanner.keyOf / fscase.Fold / sysguard.normalize /
// filter.toSlashPat），四份对"`\` 在非 Windows 上算不算分隔符"的取径并不一致，
// 而 M26 那类缺陷正是"同一判据多份实现、改一处漏三处"的产物。只收一次不写门禁，
// 下一轮会长出第五份。
//
// 判据只覆盖**非测试文件**：测试里另写一份 inline 实现是**独立基准**（前提自检
// 不得走被测读出口），不是重复的生产判据，故不该被这条门禁误伤。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// slashSwapAllowlist 是唯一被允许实现"分隔符→/ 交换"的文件。
var slashSwapAllowlist = map[string]bool{
	filepath.Join("internal", "pathnorm", "pathnorm.go"): true,
}

// retiredSlashHelpers 是收归后**不得再以函数形式存在**的旧名字（M64 退场断言）。
// 它们只允许作为调用 pathnorm 的一行包装出现（有调用者且判据在 pathnorm），
// 不允许各自持有归一逻辑；这里判的是"名字还在 = 实现可能还在"。
var retiredSlashHelpers = map[string]string{
	"keyOf":      "internal/scanner/scanner.go",
	"underKey":   "internal/scanner/scanner.go",
	"normalize":  "internal/sysguard/sysguard.go",
	"under":      "internal/sysguard/sysguard.go",
	"toSlashPat": "internal/filter/filter.go",
}

// TestSlashSwapHasSingleImplementation 扫全仓非测试 Go 文件，断言
// `strings.ReplaceAll(_, _, "/")` 只出现在白名单文件里。
func TestSlashSwapHasSingleImplementation(t *testing.T) {
	var offenders []string
	var scanned int
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if p != "." && (name[0] == '.' || name == "node_modules" || name == "dist") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		scanned++
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, p, nil, 0)
		if perr != nil {
			return perr
		}
		rel, rerr := filepath.Rel(".", p)
		if rerr != nil {
			rel = p
		}
		if slashSwapAllowlist[rel] {
			return nil
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "ReplaceAll" || len(call.Args) != 3 {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "strings" {
				return true
			}
			if lit, ok := call.Args[2].(*ast.BasicLit); !ok || lit.Value != `"/"` {
				return true
			}
			offenders = append(offenders, rel+":"+tokenPos(fset, call.Pos()))
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// fail-closed：扫不到文件说明判据本身被删空，不许当成"通过"。
	if scanned < 50 {
		t.Fatalf("只扫到 %d 个非测试 Go 文件，遍历本身失效（这条门禁的下界也是判据）", scanned)
	}
	if len(offenders) > 0 {
		t.Errorf("分隔符归一存在第二实现（I5：一处判定一处实现），应改调 internal/pathnorm：%v", offenders)
	}
	t.Logf("M64 方法面：扫描 %d 个非测试文件 / 违规 %d 处", scanned, len(offenders))
}

func tokenPos(fset *token.FileSet, p token.Pos) string {
	pos := fset.Position(p)
	return strconv.Itoa(pos.Line)
}

// TestRetiredSlashHelpersAreGone 钉住：五个旧名字不再持有函数体。
// 收归若只是"加一个新包、旧实现继续被调用"，这条会红。
func TestRetiredSlashHelpersAreGone(t *testing.T) {
	bodies := map[string][]string{}
	// 只从 "." 走一趟：分多个根走会把同一文件重复计入，读数就成了假的。
	{
		err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				name := d.Name()
				if p != "." && (name[0] == '.' || name == "node_modules" || name == "dist") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			fset := token.NewFileSet()
			file, perr := parser.ParseFile(fset, p, nil, 0)
			if perr != nil {
				return perr
			}
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				if _, want := retiredSlashHelpers[fn.Name.Name]; want {
					rel, rerr := filepath.Rel(".", p)
					if rerr != nil {
						rel = p
					}
					bodies[fn.Name.Name] = append(bodies[fn.Name.Name], rel)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(bodies) > 0 {
		var got []string
		for name, files := range bodies {
			got = append(got, name+"@"+strings.Join(files, ","))
		}
		t.Errorf("M64 收归未退场：%v 仍是独立函数体（判据应只在 internal/pathnorm 一份）", got)
	}
}
