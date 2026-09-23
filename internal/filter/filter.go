// Package filter 实现扫描过滤器（01 §1.2 / 04 M1-T06）。
package filter

import (
	"path"
	"path/filepath"
	"strings"

	"filededup/internal/fscase"
	"filededup/internal/model"
	"filededup/internal/pathnorm"
)

// Matcher 预编译的过滤器（G3）。
// 两组扩展名列表在扫描前一次性归一小写并按长度选择匹配策略，
// 取代原先「每文件对列表做 O(列表长) 次 strings.EqualFold」的线性比较。
type Matcher struct {
	f        *model.Filters
	incExt   extSet
	excExt   extSet
	excPaths []string // 2026-09-18 审查 I1：已归一为 "/" 分隔的排除模式
	// excDirs 是 ExcludeDirs 的归一键（"/" 分隔、去尾斜杠、空白项丢弃）。
	// 与 excPaths 分属两条通道：这里比绝对路径前缀，不做 glob、不看卷大小写
	// 敏感度（条目来自本机选择器，拼写即真值；错拼的后果是不命中=多扫，安全向）。
	excDirs []string
	// invalid 是**语法无效**的排除模式（原文，未归一），由 Compile 逐段探出（R2-4）。
	// 它们**仍在 excPaths 里**：本字段只负责"说出来了"，不负责改判（方向仍是少排除=多扫）。
	invalid []string
}

// extSetMapMin 建 map 的列表长度阈值（含）。
// 依据 BenchmarkExtMatch 实测（Apple M 系列，命中项置于列表末位 = 线性最坏情况）：
//
//	列表长度      新线性        新 map      (旧 EqualFold)
//	   1        0.68 ns      4.53 ns       2.96 ns
//	   4        2.00 ns      5.10 ns      10.06 ns
//	   8        4.70 ns      6.19 ns      18.40 ns
//	  12        7.10 ns      4.60 ns      26.36 ns   ← 交叉点
//	  16        8.96 ns      4.50 ns      34.34 ns
//	  64       37.48 ns      4.51 ns     134.50 ns
//
// 故 12 项以下走线性（元素已在编译期预小写，直接 == 比较），
// 12 项及以上建 map 做到与列表长度无关。
// 注意：不能一律用 map——1~8 项这一常见区间 map 反而慢于线性，
// 属于性能倒退（实测 N08：6.19 vs 4.70 ns）。
const extSetMapMin = 12

// normalizeExtList 把**用户输入侧**的扩展名归一为 filepath.Ext 的形态（FLT-1）。
//
// 为什么放在 Compile 而不是 newExtSet：newExtSet 的契约是"入参已是 .ext 形态，
// 只做小写与选表"，它另有一份与朴素实现逐位等价的回归用例
// （filter_test.go TestExtSetEquivalence）；在 newExtSet 里补点会让那份基准
// 反过来钉住错误语义。用户输入只在 Compile 这一处进来，归一放这里同样是
// 一处判定一处实现（I5），且不用改那份等价性基准。
//
// 修前的形状：用户在界面/CLI 里写 "tmp"（两处都只 trim 空格、不补点），
// 而 has() 拿到的是 filepath.Ext 的产物 ".tmp" ⇒ 永不相等 ⇒ 这条排除
// **整条 fail-open**：界面登记着、盘上一个没排、也不报错。
//
// 空白项按"未配置"处理（丢弃）：不归一它们会留下一个匹配不上任何文件的
// 空串条目，使 inactive() 判假 ⇒ 包含列表变成"一个都不放行"。
func normalizeExtList(list []string) []string {
	if len(list) == 0 {
		return list
	}
	out := make([]string, 0, len(list))
	for _, e := range list {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		out = append(out, e)
	}
	return out
}

// extSet 预编译的扩展名集合（两种表示二选一，均已在编译期小写）。
type extSet struct {
	list []string            // 短列表：线性扫（元素已小写）
	set  map[string]struct{} // 长列表：O(1) 查表（键已小写）
}

func newExtSet(list []string) extSet {
	if len(list) == 0 {
		return extSet{}
	}
	if len(list) < extSetMapMin {
		out := make([]string, len(list))
		for i, e := range list {
			out[i] = strings.ToLower(e)
		}
		return extSet{list: out}
	}
	m := make(map[string]struct{}, len(list))
	for _, e := range list {
		m[strings.ToLower(e)] = struct{}{}
	}
	return extSet{set: m}
}

// inactive 未配置（对应原「列表为空」分支）。
func (s extSet) inactive() bool { return s.list == nil && s.set == nil }

// has 判定 ext（调用方保证已小写）。
// 语义与旧 containsExt+EqualFold 等价：旧实现 ext 侧已 ToLower，
// EqualFold(e, ext) 即 ToLower(e) == ext。
func (s extSet) has(ext string) bool {
	if s.set != nil {
		_, ok := s.set[ext]
		return ok
	}
	for _, e := range s.list {
		if e == ext {
			return true
		}
	}
	return false
}

// Compile 预编译过滤器。f 为 nil 时返回 nil，
// 而 nil *Matcher 的 Apply 恒返回 true（与旧 Apply(nil, ...) 语义一致），
// 因此调用方可直接 matcher.Apply(...) 无需判空。
func Compile(f *model.Filters) *Matcher {
	if f == nil {
		return nil
	}
	var excPaths, invalid []string
	if len(f.ExcludePaths) > 0 {
		excPaths = make([]string, len(f.ExcludePaths))
		for i, pat := range f.ExcludePaths {
			norm := pathnorm.Slash(pat, "\\")
			excPaths[i] = norm
			// R2-4：语法无效的模式**照旧留在 excPaths 里**（它命中不了任何东西，
			// 行为与改前逐字一致），这里只是把它登记出来让上层能留痕。
			// ★ 判据取归一后的形态：匹配用的就是它，报"归一后的死活"才是真话；
			// 但回显给用户的仍是原文，他才认得出自己敲的是哪一条。
			if badExcludePattern(norm) {
				invalid = append(invalid, pat)
			}
		}
	}
	var excDirs []string
	for _, d := range f.ExcludeDirs {
		// 归一走 pathnorm.DirKey（平台分隔符口径，与遍历侧同一函数）；
		// 空白项按未配置丢弃：空键在 Under 下对绝对路径恒真，一条空串
		// 就等价于"排除全盘"，这一格必须钉死（见 TestExcludeDirPathInactiveIsAlwaysFalse）。
		key := pathnorm.DirKey(strings.TrimSpace(d), string(filepath.Separator))
		if key == "" {
			continue
		}
		excDirs = append(excDirs, key)
	}
	return &Matcher{
		f:        f,
		incExt:   newExtSet(normalizeExtList(f.IncludeExts)),
		excExt:   newExtSet(normalizeExtList(f.ExcludeExts)),
		excPaths: excPaths,
		excDirs:  excDirs,
		invalid:  invalid,
	}
}

// badExcludePattern 该模式是否语法无效（Go 的 path.Match 会报 ErrBadPattern）。
//
// ★ 必须**逐段**问，不能拿整条模式去问一次：整模式自检是输入相关的——
// `a[b/c]d` 用 9 个不同串试，一次都不报错，而逐段试 `a[b` 稳定报错。
// 本包的匹配本就是切段后逐段交给 path.Match（matchSegs），探针与运行时不同形就会漏。
// （22 条语料的实测对照见设计段 §30.6：只有逐段能抓到 1 条、只有整模式能抓到 0 条。）
//
// `**` 段跳过：matchSegs 把它当递归通配单独处理，从不交给 path.Match。
// 探针串必须非空：Go 在空串上提前退出 chunkScan，不报语法错。
//
// ★ 抓不到"语法合法但永不命中"（`a/[z-a]` 反向区间、`[!]a]`）：那是 glob 可满足性问题，
// 要判它就得在本包里再写一份 glob 语义，正是 P-1 静态钉防的那类分叉。这一条边界写进 docs/09。
func badExcludePattern(normPat string) bool {
	for _, seg := range strings.Split(normPat, "/") {
		if seg == "" || seg == "**" {
			continue
		}
		if _, err := matchFold(seg, "probe", false); err != nil {
			return true
		}
	}
	return false
}

// InvalidPatterns 返回本次编译里语法无效的排除模式（用户原文，按输入顺序）。
// 全部有效时返回 nil。nil *Matcher 恒返回 nil，与 Compile/Apply/ExcludeDir
// 的三处 nil 短路同契约——所以上层可以直接 matcher.InvalidPatterns() 不判空。
func (m *Matcher) InvalidPatterns() []string {
	if m == nil {
		return nil
	}
	return m.invalid
}

// Apply 判断文件是否通过过滤器。
// name 为文件名；rel 为相对扫描根的路径（用于路径 glob）；size 为字节数。
// 返回 false 表示跳过。
//
// M105（2026-09-22 裁定「按卷敏感度过渡」）：caseMode 是**这条路径所属扫描根**的卷语义。
// 只有 `Proven && !Sensitive`（FAT/exFAT/NTFS 这类确证不敏感卷）才把排除模式放宽成
// 不分大小写；未确证与确证敏感都走改前那条腿，一字不差。放宽的方向是"多排除 = 少扫"，
// 与 M62 的"少扫不错删"同向；反向（拿未确证当不敏感）才是事故。
func (m *Matcher) Apply(name, rel string, size uint64, caseMode fscase.Result) bool {
	if m == nil {
		return true
	}
	f := m.f
	// 大小区间（0 = 不限）
	if f.MinSize > 0 && size < f.MinSize {
		return false
	}
	if f.MaxSize > 0 && size > f.MaxSize {
		return false
	}
	// 隐藏文件：unix 规则以 "." 开头；目录级隐藏已在遍历时跳过
	if !f.IncludeHidden && strings.HasPrefix(name, ".") {
		return false
	}
	ext := strings.ToLower(filepath.Ext(name))
	// 扩展名包含：空 = 全部
	if !m.incExt.inactive() && !m.incExt.has(ext) {
		return false
	}
	if m.excExt.has(ext) {
		return false
	}
	// 路径排除 glob
	if len(m.excPaths) > 0 {
		insensitive := excludesRelaxed(caseMode)
		// Windows 遍历给的是 "\" 分隔，统一后再比对（模式侧同一函数 ⇒ 两侧对称）
		rel = pathnorm.Slash(rel, "\\")
		for _, pat := range m.excPaths {
			if matchPath(pat, rel, name, insensitive) {
				return false
			}
		}
	}
	return true
}

// excludesRelaxed 排除模式能否按"不分大小写"比对：**两个条件缺一不可**。
// ★ 写成 `!caseMode.Sensitive` 单条件就是把"退默认"当"实测"（M62 反对的无声改判），
// 判据格在 filter_m105_test.go 的「未确证卷上照旧区分大小写」那一格。
func excludesRelaxed(caseMode fscase.Result) bool {
	return caseMode.Proven && !caseMode.Sensitive
}

// ExcludeDir 判定目录是否可被安全剪枝（返回 true 表示不再深入遍历）。
//
// P2：修正前 ExcludePaths 只在文件级生效，排除 node_modules/.git 时仍会完整
// 遍历其内部再逐个丢弃——"排除"省下了结果却没省下时间。
//
// 剪枝比文件级过滤更激进（整棵子树消失），因此只对「命中可向后代传播」的
// 模式形态生效，见 prunable：含通配且含斜杠的模式（如 "*/build"）经 filepath.Match
// 只匹配目录自身、不匹配其后代，按它剪枝会连带丢掉本该保留的文件（实测确认）。
// 这类模式退回原有的文件级判定，遍历量不变但结果始终正确。
//
// caseMode 的口径与 Apply 同一条（M105）：两腿必须同判据，否则会出现"文件排了、
// 整棵目录没排"的分裂读数。
func (m *Matcher) ExcludeDir(rel, name string, caseMode fscase.Result) bool {
	if m == nil {
		return false
	}
	insensitive := excludesRelaxed(caseMode)
	rel = pathnorm.Slash(rel, "\\")
	for _, pat := range m.excPaths {
		if !prunable(pat) {
			continue
		}
		if matchPath(pat, rel, name, insensitive) {
			return true
		}
	}
	return false
}

// ExcludeDirPath 判定绝对目录路径 full（含其整个子树）是否命中 ExcludeDirs。
// true = 遍历层直接剪枝，不再深入。条目侧与查询侧共用 pathnorm.DirKey 一份归一
// （M64：分隔符判据只许一处实现）。nil *Matcher / 空列表恒 false，同三处 nil 短路契约。
func (m *Matcher) ExcludeDirPath(full string) bool {
	if m == nil || len(m.excDirs) == 0 {
		return false
	}
	key := pathnorm.DirKey(full, string(filepath.Separator))
	for _, d := range m.excDirs {
		if pathnorm.Under(key, d) {
			return true
		}
	}
	return false
}

// prunable 该排除模式能否安全地用于目录剪枝。
//
// 两类可以（关键是「命中可向后代传播」，与 matchPath 的既有文件级语义一致）：
//  1. 不含 "/" 的段模式（"node_modules"、"*.tmp"）：匹配按路径段进行，
//     目录名命中则该目录下所有文件的 rel 都含同名段。
//  2. 字面前缀式（"a/b"、"a/b/"）与递归尾随式（"a/b/**"）：前者按目录前缀
//     匹配全部后代，后者 ** 跨任意层，目录命中则后代必然命中。
//
// 其余不可（C2 收紧）：
//   - 尾随单 *（"a/b/*"）：单层语义，只匹配 a/b 的直接子层；按它剪枝会连带
//     丢掉 a/b/c/d 这类本该保留的更深后代（静默多删）。
//   - 通配在中间（"*/build"、"a/**/b"、"a?/dist"、"x/[ab]/build"）：模式只匹配
//     目录自身（或某一层关系），不保证命中目录的后代同样命中，按它剪枝会
//     连带丢掉本该保留的文件。这类模式退回文件级判定。
func prunable(pat string) bool {
	if !strings.Contains(pat, "/") {
		return true
	}
	if strings.Contains(pat, "**") {
		// 递归式：仅当 ** 位于末尾（"a/b/**"）才可剪枝；中间 ** 不可
		body := strings.TrimSuffix(pat, "**")
		body = strings.TrimSuffix(body, "/")
		return !strings.ContainsAny(body, "*?[")
	}
	// 不含 **：只有完全无通配的字面前缀（"a/b"、"a/b/"）才可剪枝
	return !strings.ContainsAny(pat, "*?[")
}

// matchPath 路径排除匹配（C2：区分 *（单层，不跨分隔符）与 **（递归跨层））：
//   - 模式不含 "/"：对 rel 的每个路径段（含文件名）做 path.Match
//   - 模式含 "/" 且含 "**"：段级匹配，** 跨任意层（含零层），其余段不跨层
//   - 模式含 "/" 且不含 "**"：字面前缀（"a/b"、"a/b/"）匹配全部后代；
//     否则对整条 rel 做 path.Match（* 只匹配单层，不再剥尾作前缀——
//     修正前 "a/b/*" 会被当作递归前缀误伤 a/b/c/d）
//
// I1：一律用 path.Match 而非 filepath.Match——后者在 Windows 上以 "\" 为分隔符，
// "*" 会跨越 "/" 段，"单层"语义在三平台上各不相同。pat 与 rel 已在调用点归一为
// "/" 分隔，故匹配结果与宿主平台无关。
//
// M105：insensitive=true（确证不敏感卷）时**四条腿一致**放宽。★ 漏掉字面前缀那条是本条
// 最容易重演的 fail-open：`Cache/Sessions` 这种无通配前缀恰恰是用户最常写的排除形态，
// 而它今天走 pathnorm.Under 而不是 path.Match。
func matchPath(pat, rel, name string, insensitive bool) bool {
	if pat == "" {
		return false
	}
	if !strings.Contains(pat, "/") {
		if matchFoldOK(pat, name, insensitive) {
			return true
		}
		for _, seg := range strings.Split(rel, "/") {
			if seg == "" {
				continue
			}
			if matchFoldOK(pat, seg, insensitive) {
				return true
			}
		}
		return false
	}
	if strings.Contains(pat, "**") {
		return matchSegs(strings.Split(pat, "/"), strings.Split(rel, "/"), insensitive)
	}
	// 字面前缀式（无通配）：dir 或 dir/ 递归命中后代
	if !strings.ContainsAny(pat, "*?[") {
		prefix := strings.TrimSuffix(pat, "/")
		if prefix != "" && pathnorm.Under(foldCase(rel, insensitive), foldCase(prefix, insensitive)) {
			return true
		}
		return false
	}
	if matchFoldOK(pat, rel, insensitive) {
		return true
	}
	return false
}

// foldCase 是放宽的**唯一**大小写处理点（matchFold 与字面前缀腿共用，不留第二份）。
// 用 strings.ToLower 而非 unicode/norm：不敏感卷的语义本就是"两种拼写同物"，极端
// Unicode 字符上折不齐的后果是"少排除 = 多扫一遍"，不会多删。
func foldCase(s string, insensitive bool) string {
	if insensitive {
		return strings.ToLower(s)
	}
	return s
}

// matchFoldOK 是四条匹配腿的**唯一吞错点**：语法无效的模式在这里退成"不命中"。
// 方向是少排除 = 多扫（不会多删），而这类模式在 Compile 里已经被单独上报进失败清单
// （R2-4），所以这里吞掉的是一份**已经留过痕**的错误，不是无声失效。
func matchFoldOK(pat, s string, insensitive bool) bool {
	ok, _ := matchFold(pat, s, insensitive)
	return ok
}

// matchFold 是本包唯一允许出现 path.Match 的位置（P-1 型静态钉：filter_m105_test.go 的
// TestPathMatchOnlyInsideMatchFold）。insensitive=false 时与改前逐字同一条路径。
//
// R2-4：err 不再在这里吞掉，而是原样交给调用方——Compile 的语法校验要用它，
// 且必须用**同一个函数**，否则就要在本包里再写一份 glob 解析（那正是本钉防的分叉）。
func matchFold(pat, s string, insensitive bool) (bool, error) {
	return path.Match(foldCase(pat, insensitive), foldCase(s, insensitive))
}

// matchSegs 段级 glob 匹配：pat/rel 已按 "/" 切段。** 匹配 0..n 个段
// （递归），其余段交由 matchFold（* 不跨段）。回溯实现，段数有限无性能顾虑。
func matchSegs(pat, rel []string, insensitive bool) bool {
	if len(pat) == 0 {
		return len(rel) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(rel); i++ {
			if matchSegs(pat[1:], rel[i:], insensitive) {
				return true
			}
		}
		return false
	}
	if len(rel) == 0 {
		return false
	}
	if !matchFoldOK(pat[0], rel[0], insensitive) {
		return false
	}
	return matchSegs(pat[1:], rel[1:], insensitive)
}
