// Package filter 实现扫描过滤器（01 §1.2 / 04 M1-T06）。
package filter

import (
	"path"
	"path/filepath"
	"strings"

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
	var excPaths []string
	if len(f.ExcludePaths) > 0 {
		excPaths = make([]string, len(f.ExcludePaths))
		for i, pat := range f.ExcludePaths {
			excPaths[i] = pathnorm.Slash(pat, "\\")
		}
	}
	return &Matcher{
		f:        f,
		incExt:   newExtSet(normalizeExtList(f.IncludeExts)),
		excExt:   newExtSet(normalizeExtList(f.ExcludeExts)),
		excPaths: excPaths,
	}
}

// Apply 判断文件是否通过过滤器。
// name 为文件名；rel 为相对扫描根的路径（用于路径 glob）。
// 返回 false 表示跳过。
func (m *Matcher) Apply(name, rel string, size uint64) bool {
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
		// Windows 遍历给的是 "\" 分隔，统一后再比对（模式侧同一函数 ⇒ 两侧对称）
		rel = pathnorm.Slash(rel, "\\")
		for _, pat := range m.excPaths {
			if matchPath(pat, rel, name) {
				return false
			}
		}
	}
	return true
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
func (m *Matcher) ExcludeDir(rel, name string) bool {
	if m == nil {
		return false
	}
	rel = pathnorm.Slash(rel, "\\")
	for _, pat := range m.excPaths {
		if !prunable(pat) {
			continue
		}
		if matchPath(pat, rel, name) {
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
func matchPath(pat, rel, name string) bool {
	if pat == "" {
		return false
	}
	if !strings.Contains(pat, "/") {
		if ok, _ := path.Match(pat, name); ok {
			return true
		}
		for _, seg := range strings.Split(rel, "/") {
			if seg == "" {
				continue
			}
			if ok, _ := path.Match(pat, seg); ok {
				return true
			}
		}
		return false
	}
	if strings.Contains(pat, "**") {
		return matchSegs(strings.Split(pat, "/"), strings.Split(rel, "/"))
	}
	// 字面前缀式（无通配）：dir 或 dir/ 递归命中后代
	if !strings.ContainsAny(pat, "*?[") {
		prefix := strings.TrimSuffix(pat, "/")
		if prefix != "" && pathnorm.Under(rel, prefix) {
			return true
		}
		return false
	}
	if ok, _ := path.Match(pat, rel); ok {
		return true
	}
	return false
}

// matchSegs 段级 glob 匹配：pat/rel 已按 "/" 切段。** 匹配 0..n 个段
// （递归），其余段交由 path.Match（* 不跨段）。回溯实现，段数有限无性能顾虑。
func matchSegs(pat, rel []string) bool {
	if len(pat) == 0 {
		return len(rel) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(rel); i++ {
			if matchSegs(pat[1:], rel[i:]) {
				return true
			}
		}
		return false
	}
	if len(rel) == 0 {
		return false
	}
	if ok, _ := path.Match(pat[0], rel[0]); !ok {
		return false
	}
	return matchSegs(pat[1:], rel[1:])
}
