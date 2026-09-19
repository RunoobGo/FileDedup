//go:build darwin || linux

package scanner

import (
	"os"
	"syscall"

	"filededup/internal/model"
)

// keyFromInfo unix 平台：lstat 的 Stat_t 顺带携带 inode/dev，零额外成本。
func keyFromInfo(path string, info os.FileInfo) model.FileKey {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return model.FileKey{
			VolumeID:  uint64(st.Dev),
			FileIndex: uint64(st.Ino),
			Resolved:  true,
		}
	}
	return model.FileKey{}
}

// ResolveKey unix 平台已解析，直接返回。
func ResolveKey(e *model.FileEntry) model.FileKey {
	return e.Key
}
