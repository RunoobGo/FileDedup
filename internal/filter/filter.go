// Package filter 实现扫描过滤器（01 §1.2 / 04 M1-T06）。
package filter

import (
	"path/filepath"
	"strings"

	"filededup/internal/model"
)

// Matcher 预编译的过滤器（G3）。
// 两组扩展名列表在扫描前一次性归一小写并按长度选择匹配策略，
// 取代原先「每文件对列表做 O(列表长) 次 strings.EqualFold」的线性比较。
type Matcher struct {
	f      *model.Filters
	incExt extSet
	excExt extSet
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
	return &Matcher{
		f:      f,
		incExt: newExtSet(f.IncludeExts),
		excExt: newExtSet(f.ExcludeExts),
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
	for _, pat := range f.ExcludePaths {
		if matchPath(pat, rel, name) {
			return false
		}
	}
	return true
}

// matchPath 路径排除匹配：
//   - 模式不含 "/"：对 rel 的每个路径段（含文件名）做 filepath.Match
//   - 模式含 "/"：去掉尾部 "**"/"*" 后作前缀匹配，或对 rel 做 filepath.Match
func matchPath(pat, rel, name string) bool {
	if pat == "" {
		return false
	}
	if !strings.Contains(pat, "/") {
		if ok, _ := filepath.Match(pat, name); ok {
			return true
		}
		for _, seg := range strings.Split(rel, "/") {
			if seg == "" {
				continue
			}
			if ok, _ := filepath.Match(pat, seg); ok {
				return true
			}
		}
		return false
	}
	// 前缀式：dir/** 或 dir/
	prefix := strings.TrimSuffix(pat, "**")
	prefix = strings.TrimSuffix(prefix, "*")
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix != "" {
		if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
			return true
		}
	}
	if ok, _ := filepath.Match(pat, rel); ok {
		return true
	}
	return false
}
