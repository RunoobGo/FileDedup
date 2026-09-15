// Package filter 实现扫描过滤器（01 §1.2 / 04 M1-T06）。
package filter

import (
	"path/filepath"
	"strings"

	"filededup/internal/model"
)

// Apply 判断文件是否通过过滤器。
// path 为绝对路径；rel 为相对扫描根的路径（用于路径 glob）。
// 返回 false 表示跳过。
func Apply(f *model.Filters, name, rel string, size uint64) bool {
	if f == nil {
		return true
	}
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
	if len(f.IncludeExts) > 0 && !containsExt(f.IncludeExts, ext) {
		return false
	}
	if containsExt(f.ExcludeExts, ext) {
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

func containsExt(list []string, ext string) bool {
	for _, e := range list {
		if strings.EqualFold(e, ext) {
			return true
		}
	}
	return false
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
