//go:build darwin || linux

package fsid

import (
	"os"
	"syscall"
)

func fromInfo(info os.FileInfo) ID {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ID{}
	}
	return ID{
		Dev:      uint64(st.Dev),
		Ino:      uint64(st.Ino),
		CtimeNs:  ctimeNs(st),
		Resolved: true,
	}
}

// FromPathNoFollow 取 path **自身**的物理身份，不跟随符号链接。
//
// unix 上 lstat 的结果就带 (dev, ino)，直接可用——与 Windows 侧行为等价，
// 调用方无需分平台。
func FromPathNoFollow(path string) (ID, error) {
	li, err := os.Lstat(path)
	if err != nil {
		return ID{}, err
	}
	return fromInfo(li), nil
}
