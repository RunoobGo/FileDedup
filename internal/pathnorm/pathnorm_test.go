package pathnorm

import (
	"strings"
	"testing"
)

// 基准按**旧实现的语义**独立重写一份，不走被测读出口：
// 收归这一类改动的风险不是"新函数算错"，而是"新函数与四份旧函数里的某一份不一致"，
// 所以每条旧规则都要有自己的对照物（04 §6.8.0：前提自检必须独立于被测物）。

// goldSwap 是 scanner.keyOf 的原语义：只换 sep 传入的字符，无 fast path、无空串守卫。
func goldSwap(p, sep string) string {
	if sep == "" {
		return p // 旧 keyOf 在生产里从不传空 sep；这一支是为了能对照而设
	}
	return strings.ReplaceAll(p, sep, "/")
}

// goldSwapBackslash 是 fscase.Fold / filter.toSlashPat 的替换腿原语义（恒换 "\"）。
func goldSwapBackslash(p string) string {
	if !strings.Contains(p, "\\") {
		return p
	}
	return strings.ReplaceAll(p, "\\", "/")
}

// goldNormalize 是 sysguard.normalize 的原语义（恒换 + 去尾斜杠、根保留）。
func goldNormalize(p string) string {
	q := goldSwapBackslash(p)
	for len(q) > 1 && strings.HasSuffix(q, "/") {
		q = q[:len(q)-1]
	}
	return q
}

// slashCorpus 覆盖三类分歧：Windows 风格串、unix 上含合法反斜杠的文件名、空串与纯分隔符。
var slashCorpus = []string{
	"", "/", "//", "\\", "\\\\",
	"C:\\a\\b", `C:\a`, `C:\`, `C:\a\`,
	`/a/b`, `/a/b/`, `/a//b//`,
	`a\b`, `a\b\c`, `.fdd-tmp\x`,
	`\\server\share\x`,
	"/System/Volumes/Data", "/private/var/folders",
}

func TestSlashMatchesPlatformSepSemantics(t *testing.T) {
	for _, sep := range []string{`\`, "/", ""} {
		for _, p := range slashCorpus {
			if got, want := Slash(p, sep), goldSwap(p, sep); got != want {
				t.Errorf("Slash(%q, %q) = %q, want %q（旧 keyOf 语义）", p, sep, got, want)
			}
		}
	}
}

// TestSlashBackslashLegMatchesOldThreeCopies 钉住：给字面 "\\" 时本函数与
// fscase.Fold / filter.toSlashPat / sysguard.normalize 的替换腿逐字相同。
// 这三份旧实现今天的行为一致性就是"零语义迁移"的全部内容。
// ★ 2026-09-22 M63 后本句对 **fscase.Fold 已不成立**（它改注入宿主分隔符，见 §27.3）：
// 上面那份 gold 仍等价于 sysguard/filter 那两处刻意保留的字面 "\"，对 Fold 只在
// Windows 上等价。本用例不吃 Fold，所以断言不动；改的是这句"今天"的范围。
func TestSlashBackslashLegMatchesOldThreeCopies(t *testing.T) {
	for _, p := range slashCorpus {
		got, want := Slash(p, `\`), goldSwapBackslash(p)
		if got != want {
			t.Errorf("恒换腿不等价：Slash(%q, `\\`) = %q, want %q", p, got, want)
		}
	}
}

// TestTrimTailKeepRootMatchesSysguardNormalize 把 sysguard.normalize 的完整
// 旧语义（恒换 + 保根去尾）作为对照物，逐条比对本包两条腿的合成结果。
func TestTrimTailKeepRootMatchesSysguardNormalize(t *testing.T) {
	for _, p := range slashCorpus {
		got, want := TrimTailKeepRoot(Slash(p, `\`)), goldNormalize(p)
		if got != want {
			t.Errorf("normalize 收归不等价：输入 %q 得 %q, want %q", p, got, want)
		}
	}
}

func TestUnder(t *testing.T) {
	cases := []struct {
		child, parent string
		want          bool
		why           string
	}{
		{"/a/b", "/a", true, "直接后代"},
		{"/a/b/c", "/a", true, "隔代后代"},
		{"/a", "/a", true, "自身"},
		{"/ab", "/a", false, "同前缀但不同段——必须判否"},
		{"/a/b", "/a/b/c", false, "父比子长"},
		{"/a", "", true, "空父即整卷（ops.keep 依赖，见 TestUnderEmptyParentMeansWholeVolume）"},
		{"a/b", "", false, "相对路径不算在盘根内"},
		{"/data/ab", "/data/a", false, "只差一个分隔符也不是后代——M64 变异 M-M64-c 的靶子"},
	}
	for _, c := range cases {
		if got := Under(c.child, c.parent); got != c.want {
			t.Errorf("Under(%q, %q) = %v, want %v（%s）", c.child, c.parent, got, c.want, c.why)
		}
	}
}

// TestUnderEmptyParentMeansWholeVolume 钉住 ops.keep 依赖的那条语义：
// Clean("/") 剥掉唯一斜杠后得到空串，此时任何绝对路径都算"在该目录内"。
// 这条不是顺手写出来的，是 keep.go:320-323 注释里明写的取向。
func TestUnderEmptyParentMeansWholeVolume(t *testing.T) {
	for _, child := range []string{"/", "/a", "/a/b"} {
		if !Under(child, "") {
			t.Errorf("Under(%q, \"\") 应为 true：空前缀即整卷（ops.keep 的保留目录判据依赖此语义）", child)
		}
	}
	if Under("a/b", "") {
		t.Errorf("Under(\"a/b\", \"\") 应为 false：相对路径不该被算进盘根")
	}
}

// dirKeyCorpus 是 A3 那四格形状（盘符 / UNC 全形 / 尾分隔符 / 相对形）的落点。
//
// ★ 出处与自纠：`docs/05` §0.2 A3 原文写的是"前端 pathpolicy 混例（盘符 / UNC /
// 尾分隔符 / 相对路径）是 node 用例，不必真机"。现读两处都不成立——
// ① `frontend/src/utils/pathpolicy.ts` 只剩 `isGroupCrossVolume` 一条判据，输入是后端
//
//	算好的卷标识（`vid:<id>` / `root:<VolumeName>`），**函数体不解析任何路径**
//	（D-1 已把那份路径判据删掉，因为它是在 `internal/ops/keep.go` 之外的第二份实现，
//	正是 M64 那一族）；node 面现有 8 条用例（`frontend/tests/pathpolicy-crossvolume.test.ts`
//	U-1~U-8），盘符那一格保留为 U-6。
//
// ② 设计段 §7.2 那张覆盖表里"UNC 只有根、没有 `\\server\share` 全形"（△）与
//
//	"相对路径零命中"（✗）**两格是错的**——本文件现读 `:42-43` 就有 `a\b`、`a\b\c`、
//	`\\server\share\x`。落笔前被现读推翻，按 §6.23 二·1 的规矩就地撤回，不写进账。
//
// ⇒ 真正没有直接读数的只有一处：**`DirKey` 零直接用例**（现读 `grep -rn "DirKey" --include=*.go`
//
//	只有定义 `pathnorm.go:71` 与 `internal/filter/filter.go:150`、`:289` 两个调用点）。
//	它是一条**承重函数**：ExcludeDirs 的条目侧与遍历侧共用它做归一（:283 的注释原话），
//	而"两侧口径不一致"就是 M64 立这个包的理由。
//
// ★ 本用例钉的是**不解释**：DirKey 只做"换 sep 那一字符 + 保根去尾"，
//
//	不吞根、不补根、不猜绝对、不认 Windows 形状。每一条 `want` 都是这个意思。
func TestDirKeyShapesAreNotInterpreted(t *testing.T) {
	cases := []struct {
		p, sep, want, why string
	}{
		// —— 盘符：只换字符，不认"这是一个卷" ——
		{`C:\a\b`, `\`, `C:/a/b`, "盘符条目：换字符后就是普通键"},
		{`C:\a\b`, "/", `C:\a\b`, "非 Windows 宿主的 sep 给 `/`：反斜杠是合法文件名字符，不得被吞"},
		{`C:\`, `\`, `C:`, "盘符根去尾后是 `C:` 而不是 `C:/`——TrimTailKeepRoot 只保 len==1 的那个 `/`（unix 盘根），Windows 盘根不在它的保护范围内。★ 这一格设计段初稿写的是保住尾斜杠，落笔前被现读推翻"},

		// —— UNC 全形：前导双分隔符原样保留，不折成单根 ——
		{`\\server\share\a\`, `\`, `//server/share/a`, "UNC 全形带尾分隔符：只去尾那一个"},
		{`\\server\share`, `\`, `//server/share`, "UNC 无前导尾杠：两个前导字符都在（不折成 /server/share）"},
		{`\\server\share\`, `\`, `//server/share`, "UNC 带尾杠与上一行必须同键——ExcludeDirs 两种写法同义"},

		// —— 尾分隔符：条目侧写过与没写过要同键 ——
		{`/a/b`, `\`, `/a/b`, "绝对 unix 形：无命中，原样返回"},
		{`/a/b/`, `\`, `/a/b`, "尾斜杠写没写过同键"},
		{`/a//b//`, `\`, `/a//b`, "只去**尾部连续**的，中间的双分隔符不归一（那是另一个判据，不在这条里）"},
		{`/`, `\`, `/`, "盘根：len>1 才去，所以根永远还是 `/`——去光了'保护盘根'就与'什么都不保护'同形"},

		// —— 相对形：不补根、不猜绝对 ——
		{`a\b\c`, `\`, `a/b/c`, "相对 Windows 形：换字符，但**不**补前导 `/`"},
		{`./a/b/`, `\`, `./a/b`, "相对点形：只去尾，不做 Clean（做了就长出第五份实现）"},
		{`..\a\b`, `\`, `../a/b`, "上跳形同样原样交给键空间——归一不是本包的职责"},

		// —— 空 sep 守卫与空条目 ——
		{`C:\a\`, "", `C:\a\`, "sep 传空串 ⇒ 原样返回（Slash:27-28 明写这是必须守卫而非防御）"},
		{"", `\`, "", "空条目得空键 ⇒ 丢弃是消费方的判据（见 TestDirKeyEmptyKeyIsWhyCallerMustDrop）"},
	}
	for _, c := range cases {
		if got := DirKey(c.p, c.sep); got != c.want {
			t.Errorf("DirKey(%q, %q) = %q, want %q（%s）", c.p, c.sep, got, c.want, c.why)
		}
	}
	// 上一表里 `C:\` → `C:` 那一格的**后果**必须当场读到：盘根键被去尾之后仍要能当父键用，
	// 否则"去尾"就不是无害归一而是把 Windows 盘根条目废掉。键空间里 Windows 路径恒是 `C:/…` 形，
	// 所以 `C:` ＋ Under 的 `parent+"/"` 恰好接得上——这一条就是这个组合成立的证明。
	if !Under(DirKey(`C:\a\b`, `\`), DirKey(`C:\`, `\`)) {
		t.Errorf("盘根条目归一后必须仍能圈住同卷子路径：Under(%q, %q) 判否",
			DirKey(`C:\a\b`, `\`), DirKey(`C:\`, `\`))
	}
}

// TestDirKeyEmptyKeyIsWhyCallerMustDrop 钉住包注释 :69-70 那句"丢弃判据归消费方"：
// 空键在 Under 里对**任何绝对路径恒真**，所以"什么都不排除"与"全盘排除"会长成同一个样子。
// 本用例不测 DirKey 的输出（那由上表钉），测的是这条危险形状确实存在 ⇒ 消费方
// （internal/filter/filter.go:150 的条目侧）必须丢弃空白条目，且不许由 DirKey 代劳。
func TestDirKeyEmptyKeyIsWhyCallerMustDrop(t *testing.T) {
	if DirKey("   ", `\`) != "   " {
		t.Fatal(`DirKey 不得替消费方做 TrimSpace——它连"空白"这一判据都不该有`)
	}
	if !Under("/etc/passwd", DirKey("", `\`)) {
		t.Errorf("空键对绝对路径恒真——这正是消费方必须丢弃空白条目的理由，别把它改成'不恒真'")
	}
}

// TestDirKeyMatchesTwoLegComposition 是"两条腿合成"的对照：本包三条判据
// （Slash + TrimTailKeepRoot）合成出的 DirKey 必须逐字等于 sysguard.normalize 的旧语义。
// 上面的表是**形状**判据，这条是**等价**判据——形状表可以被一份错的天真实现满足，
// 等价腿不能（04 §6.8.0：前提自检必须独立于被测物）。
func TestDirKeyMatchesTwoLegComposition(t *testing.T) {
	for _, p := range slashCorpus {
		if got, want := DirKey(p, `\`), goldNormalize(p); got != want {
			t.Errorf("DirKey(%q, `\\`) = %q, want %q（sysguard.normalize 旧语义）", p, got, want)
		}
	}
}
