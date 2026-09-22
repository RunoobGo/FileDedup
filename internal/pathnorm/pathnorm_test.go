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
