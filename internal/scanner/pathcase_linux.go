//go:build linux

package scanner

// foldPath ext4 默认大小写敏感：原样比较。
func foldPath(p string) string { return p }
