//go:build !windows

package fsid

import "os"

// fromFile 非 Windows：Stat_t 已在 fstat 结果里，直接复用 FileInfo 路径。
func fromFile(f *os.File) ID {
	st, err := f.Stat()
	if err != nil {
		return ID{}
	}
	return fromInfo(st)
}
