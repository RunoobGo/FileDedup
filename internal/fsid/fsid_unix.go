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
