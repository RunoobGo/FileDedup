//go:build darwin || windows

package scanner

import "strings"

// foldPath mac/win 默认大小写不敏感文件系统：路径折叠比较。
func foldPath(p string) string { return strings.ToLower(p) }
