package filter

// R2-4（2026-09-23 第四轮全仓审查，设计段 §30.6）：坏 glob 不许再无声失效。
//
// 现象：path.Match 对语法无效的模式返回 (false, ErrBadPattern)，而这条 err 在
// matchFold 里被 `_` 吞掉 ⇒ 用户写的那一条排除**整条不生效**，且不留任何痕迹。
// 方向上是安全的（命中不了 = 少排除 = 多扫，与 M62/M105 的"多扫不错删"同向），
// 但"计数说真话"这一条纪律不允许它静默。
//
// ★ 本文件的判据来自本机实测（§30.6 的 22 条语料表），不是照 Go 文档推的：
//   - 必须**逐段**探：`a[b/c]d` 拿 9 个串去试整模式一次都不报错，逐段探稳定报坏段 `a[b`。
//     本包的匹配本就是切段后逐段交给 path.Match（matchSegs），探针与运行时不同形就会漏。
//   - 探针串必须非空：Go 在空串上提前退出 chunkScan，不报语法错。
//   - 抓不到"语法合法但永不命中"：`a/[z-a]`（反向区间）、`[!]a]` 在 Go 里都不是错误。

import (
	"strings"
	"testing"

	"filededup/internal/model"
)

// TestCompileFlagsBadPatterns 钉住"哪些算语法无效"。
//
// 正例逐条给了它"为什么算坏"，反例是本项**不承诺**的三类：反向区间、字符类写法、
// 以及正常模式——把它们收在一张表里，是为了让"只报语法错"这条边界是**读得出来的**，
// 而不是写在注释里的一句愿望。
func TestCompileFlagsBadPatterns(t *testing.T) {
	cases := []struct {
		name string
		pat  string
		want bool // true = 必须被报成语法无效
		why  string
	}{
		{"未闭合左括号", "[", true, "path.Match 对任何串都报 ErrBadPattern"},
		{"末段未闭合", "a/[/b", true, "段 `[` 坏"},
		{"星号后未闭合", "*[", true, "段 `*[` 坏"},
		{"区间起点截断", "x/**/[", true, "`**` 跳过、段 `[` 坏"},
		{"字符类无右括号", "[]a]", true, "Go 也判坏（实测）"},
		{"首段未闭合", "a[b/c]d", true, "★ 整模式自检会漏这一条，只有逐段能抓到"},
		{"归一后剩空段", `\`, false, "归一成 '/' ⇒ 两段皆空，归一后的世界里没有坏段"},
		{"转义左括号", `a\[`, true, "归一后是 `a/` + 段 `[`：报的是**归一后**的死活，" +
			"而这条模式在归一后确实永不命中 ⇒ 不是误伤"},

		{"正常：递归尾随", "build/**", false, ""},
		{"正常：段名", "node_modules", false, ""},
		{"正常：文件名 glob", "*.tmp", false, ""},
		{"正常：单层", "a/b/*", false, ""},
		{"反向区间", "a/[z-a]", false, "Go 视为**合法语法**，本项不承诺抓到（§30.6 校正计划原文）"},
		{"字符类含取反", "[!]a]", false, "Go 视为合法语法"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := Compile(&model.Filters{ExcludePaths: []string{c.pat}})
			got := m.InvalidPatterns()
			if hit := len(got) > 0; hit != c.want {
				t.Fatalf("InvalidPatterns(%q) = %v，want 命中=%v（%s）", c.pat, got, c.want, c.why)
			}
			if c.want && (len(got) != 1 || got[0] != c.pat) {
				t.Fatalf("InvalidPatterns(%q) = %v，want 原样回显这一条", c.pat, got)
			}
		})
	}
}

// TestCompileAllCleanIsSilent 负控制（AS-K 那一族）：一条坏模式都没有时，
// 必须一条都不报。少了这一格，上面那张表等于"什么都报"也能过。
func TestCompileAllCleanIsSilent(t *testing.T) {
	m := Compile(&model.Filters{ExcludePaths: []string{"build/**", "node_modules", "*.tmp", "a/b/*", "a/[z-a]", ""}})
	if got := m.InvalidPatterns(); len(got) != 0 {
		t.Fatalf("全是正常模式却报了 %v（负控制失效：本包把校验写成了无条件上报）", got)
	}
}

// TestCompileReportsRawPattern 报的是**用户输入的原文**，不是归一后的形态。
//
// Compile 会把 `\` 换成 `/`（pathnorm.Slash），归一后 `mydir\[` 变成 `mydir/` + 段 `[`，
// 校验按归一后判（匹配用的就是归一形态，报"归一后的死活"才是真话），
// 但回显必须让用户认得出自己敲的是哪一条。
func TestCompileReportsRawPattern(t *testing.T) {
	raw := `mydir\[`
	m := Compile(&model.Filters{ExcludePaths: []string{raw, "node_modules", "bad/["}})
	got := m.InvalidPatterns()
	want := []string{raw, "bad/["}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("InvalidPatterns = %v, want %v（顺序按输入顺序，且回显原文）", got, want)
	}
}

// TestBadPatternStillFailsOpen 钉住"方向不变"这条承诺。
//
// 用户本想排除 build/x，却写成了 `build/[`。坏模式**仍然留在 excPaths 里**、
// 仍然永不命中 ⇒ 该文件照旧被扫到。这一格防的是"顺手把坏模式从列表里剔掉"
// 之类的改法：剔掉与留着在此刻结果相同，但它把校验从"报告"变成了"改判"，
// 下一次改动就会有人以为这条腿有剔除语义。
func TestBadPatternStillFailsOpen(t *testing.T) {
	m := Compile(&model.Filters{ExcludePaths: []string{"build/["}})
	if len(m.InvalidPatterns()) != 1 {
		t.Fatalf("夹具失效：%q 没被报成坏模式", "build/[")
	}
	// ★ 钉的是"没被剔掉"这件结构事实，不只是"结果碰巧一样"：
	// 归一后那条模式仍在 excPaths 里，与改前逐字同一条腿。
	if strings.Join(m.excPaths, "|") != "build/[" {
		t.Fatalf("excPaths = %v，want 原样保留坏模式（校验只许报告，不许改判）", m.excPaths)
	}
	if !m.Apply("f.o", "build/x/f.o", 10, provenSensitive) {
		t.Fatal("坏模式把文件排除了：fail-open 方向被改（本项只许报告，不许改判）")
	}
	// 同一条路径换成正常模式则确实被排除——证明上面那个"通过"不是夹具根本没生效。
	ok := Compile(&model.Filters{ExcludePaths: []string{"build/**"}})
	if ok.Apply("f.o", "build/x/f.o", 10, provenSensitive) {
		t.Fatal("夹具反了：`build/**` 本该排除 build/x/f.o")
	}
}

// TestInvalidPatternsNilMatcherSafe 与 Compile/Apply/ExcludeDir 同一条 nil 短路契约。
// scanner 侧直接 matcher.InvalidPatterns() 不判空，靠的就是这一条。
func TestInvalidPatternsNilMatcherSafe(t *testing.T) {
	var m *Matcher
	if got := m.InvalidPatterns(); got != nil {
		t.Fatalf("nil *Matcher 的 InvalidPatterns = %v, want nil", got)
	}
	if Compile(nil).InvalidPatterns() != nil {
		t.Fatal("Compile(nil) 必须仍是 nil *Matcher 且取不到坏模式（不 panic 即可）")
	}
}
