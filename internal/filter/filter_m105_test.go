package filter

// M105（2026-09-22 裁定「按卷敏感度过渡」，设计稿 §28.3）：用户排除模式的段匹配今天走
// `path.Match`，**大小写敏感**，而系统守卫那条腿用 `strings.EqualFold`。后果是不敏感卷
// （FAT/exFAT/NTFS）上排除 `Temp` 挡不住盘上的 `TEMP` —— 用户写了排除、界面没说错、
// 盘上一个没排，是 fail-open（04 §6.11 FLT-3）。
//
// 放宽只给**确证不敏感**那一格（`Proven && !Sensitive`）。★ 二态布尔不够用：
// `{Sensitive:false}` 既可能是"确证不敏感"也可能是"探针失败退默认"，把后者当前者放宽
// 就是 M62 反对的那类"无声放宽"（用户没要求过的排除范围随证据缺失而扩大）⇒ 三态
// （§28.2）是本条的前提。
//
// matchPath 有**四条**判定腿（段模式 / 字面前缀 / 递归 ** / 单层通配含斜杠），
// 放宽必须四条一致 —— 只改 `path.Match` 那几处会漏掉字面前缀那条，而
// `Cache/Sessions` 这种无通配前缀恰好是用户最常写的排除形态（§28.3 的落点清单就漏了它，
// 记在 specs §28.9）。四腿各钉一格，每格三个方向：① 确证不敏感 ⇒ 必须挡（改前红）；
// ② 未确证 ⇒ 必须**不**挡（反向钉）；③ 确证敏感 ⇒ 照旧不挡（负控制，防两腿接反）。
// 全在纯逻辑里（无 build tag、不碰盘、夹具只造 Result 值）= H6 惯例，Linux CI 也真跑。

import (
	"os"
	"strings"
	"testing"

	"filededup/internal/fscase"
	"filededup/internal/model"
)

// 三格共用的卷语义速记（拿不到的证据不算证据：unproven 是零值）。
var (
	provenInsensitive = fscase.Result{Sensitive: false, Proven: true} // FAT/exFAT/NTFS
	provenSensitive   = fscase.Result{Sensitive: true, Proven: true}  // 写探针实测确证
	unproven          = fscase.Result{}                               // 探针不可用 + 卷型读不到
)

// excludeLegs 四条判定腿各一行。rel 是"盘上的实际拼写"（与模式只差大小写），
// sameCase 是"与模式同拼写"的那条 —— 它只用于前提自检，证明这条夹具测的是大小写
// 而不是模式本身写错（I5：不许"我猜它能命中"）。
var excludeLegs = []struct {
	name     string
	pat      string
	rel      string
	sameCase string
	file     string
}{
	{"段模式（不含斜杠）", "Temp", "TEMP/x.log", "Temp/x.log", "x.log"},
	{"字面前缀（含斜杠无通配）", "Cache/Sessions", "CACHE/sessions/a.log", "Cache/Sessions/a.log", "a.log"},
	{"递归 **（跨层）", "Cache/**", "CACHE/sessions/a.log", "Cache/sessions/a.log", "a.log"},
	{"单层通配含斜杠", "*/Temp", "A/TEMP", "A/Temp", "TEMP"},
}

func TestExcludeRelaxesAllFourMatchLegsOnlyOnProvenInsensitiveVolume(t *testing.T) {
	for _, leg := range excludeLegs {
		t.Run(leg.name, func(t *testing.T) {
			m := Compile(&model.Filters{ExcludePaths: []string{leg.pat}})

			// 前提自检：同拼写时必须挡住 ⇒ 下面那三格测的确实是大小写。
			if m.Apply(leg.file, leg.sameCase, 10, unproven) {
				t.Fatalf("夹具前提不成立：模式 %q 连同拼写的 %q 都没挡住，本条测不到放宽那一格", leg.pat, leg.sameCase)
			}

			// ① 改前红在这一格：确证不敏感卷上必须按不分大小写命中。
			if m.Apply(leg.file, leg.rel, 10, provenInsensitive) {
				t.Errorf("不敏感卷上排除模式仍区分大小写：Apply(%q, %q) 放行了（M105 的 fail-open）", leg.file, leg.rel)
			}
			// ② 未确证必须**不**放宽。
			if !m.Apply(leg.file, leg.rel, 10, unproven) {
				t.Errorf("卷语义未确证却放宽了：Apply(%q, %q) 被排除（把「退默认」当「实测不敏感」）", leg.file, leg.rel)
			}
			// ③ 负控制：确证敏感的卷上大小写照旧区分（敏感卷上 Temp 与 TEMP 真是两个目录）。
			if !m.Apply(leg.file, leg.rel, 10, provenSensitive) {
				t.Errorf("确证敏感卷上 %q 竟挡住了 %q ⇒ 区分大小写那条腿被反向接掉了", leg.pat, leg.rel)
			}
		})
	}
}

// TestExcludeDirPrunesOnlyOnProvenInsensitiveVolume 钉剪枝腿（M105 的症状大头就在这一腿：
// 整棵子树照扫）。只取 prunable 的三种形态（单层通配含斜杠按 C2 不剪枝，另一条用例已钉，
// 不在这里重复）。
func TestExcludeDirPrunesOnlyOnProvenInsensitiveVolume(t *testing.T) {
	cases := []struct {
		name    string
		pat     string
		rel     string
		dir     string
		relSame string
	}{
		{"段模式", "Temp", "TEMP", "TEMP", "Temp"},
		// 字面前缀腿只看 rel（name 不参与），所以前提那格必须是**同拼写的前缀**本身，
		// 写成 "Cache/sessions" 是另一回事（那不是大小写放宽，是前缀压根不匹配）。
		{"字面前缀", "Cache/Sessions", "CACHE/sessions", "sessions", "Cache/Sessions"},
		{"递归 **", "Cache/**", "CACHE/sessions", "sessions", "Cache/sessions"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := Compile(&model.Filters{ExcludePaths: []string{c.pat}})
			if !m.ExcludeDir(c.relSame, c.dir, unproven) {
				t.Fatalf("夹具前提不成立：同拼写的 %q 没被剪枝，本条测不到放宽那一格", c.relSame)
			}
			if !m.ExcludeDir(c.rel, c.dir, provenInsensitive) {
				t.Errorf("不敏感卷上目录剪枝腿没跟上：ExcludeDir(%q) = false，整棵照扫", c.rel)
			}
			if m.ExcludeDir(c.rel, c.dir, unproven) {
				t.Errorf("未确证卷上剪掉了 %q ⇒ 排除范围随证据缺失而扩大", c.rel)
			}
			if m.ExcludeDir(c.rel, c.dir, provenSensitive) {
				t.Errorf("敏感卷上不该剪掉 %q（盘上它与另一种拼写是两个目录）", c.rel)
			}
		})
	}
}

// 放宽是"多挡"，不是"改判"：原样大小写那一腿在三种卷语义下都必须继续命中。
func TestExcludeStillMatchesExactCaseAfterRelax(t *testing.T) {
	m := Compile(&model.Filters{ExcludePaths: []string{"Temp"}})
	for _, tc := range []struct {
		name string
		mode fscase.Result
	}{
		{"确证不敏感", provenInsensitive},
		{"未确证", unproven},
		{"确证敏感", provenSensitive},
	} {
		if m.Apply("x.log", "Temp/x.log", 10, tc.mode) {
			t.Errorf("%s 卷上原样大小写反而不命中 ⇒ 放宽把既有那一腿弄丢", tc.name)
		}
	}
}

// TestPathMatchOnlyInsideMatchFold 是本包的 P-1 型静态防分叉钉（M64 的 I5 口径）：
// 大小写放宽只能有**一份**实现。四条腿一旦有人各自 ToLower，就会出现"文件排了、目录没排"
// 这类分裂读数，而分裂在本仓有过先例（§16.0-1：四份 \→/ 归一实现的规则差异）。
// 判据按源码取：matchFold 之外出现 `path.Match(` 即为分叉。
//
// ★ 为什么是读源码而不是比行为：两份实现恰好一致时行为读不出差别，而一致性会随下一次
// 改动悄悄破掉。手法先例：M30 比对器读 TS 源文件、M134/M142 静态查门禁脚本。
func TestPathMatchOnlyInsideMatchFold(t *testing.T) {
	src, err := os.ReadFile("filter.go")
	if err != nil {
		t.Fatalf("读 filter.go: %v", err)
	}
	s := string(src)
	const call = "path.Match("
	total := strings.Count(s, call)
	if total == 0 {
		t.Fatal("本包一处 path.Match 都没有 ⇒ 判据失效（匹配腿被换成了别的东西，先看是不是漏改本钉）")
	}
	start := strings.Index(s, "func matchFold(")
	if start < 0 {
		t.Fatal("找不到 matchFold ⇒ 放宽的实现被搬走或改名，本钉的前提没了")
	}
	// matchFold 的函数体：从它的签名起，到下一个顶层 func 之前。
	end := strings.Index(s[start:], "\nfunc ")
	if end < 0 {
		end = len(s) - start
	} else {
		end += start
	}
	if inside := strings.Count(s[start:end], call); inside != total {
		var offenders []string
		for _, line := range strings.Split(s[:start], "\n") {
			if strings.Contains(line, call) {
				offenders = append(offenders, strings.TrimSpace(line))
			}
		}
		t.Fatalf("path.Match 必须只在 matchFold 里：包内 %d 处、matchFold 内 %d 处 ⇒ 多出的是第二份实现：%v",
			total, inside, offenders)
	}
}
