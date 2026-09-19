//go:build linux

package fsid

import "syscall"

func ctimeNs(st *syscall.Stat_t) int64 {
	return st.Ctim.Sec*1e9 + st.Ctim.Nsec
}
