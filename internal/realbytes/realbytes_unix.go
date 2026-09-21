//go:build darwin || linux

package realbytes

import (
	"os"
	"syscall"
)

// Reported unix：st_blocks × 512。数据就在遍历期 de.Info() 的 Stat_t 里，
// **零额外 syscall**——这也是选 st_blocks 而非 FIEMAP/fctl 的一个附带理由。
//
// 语义边界（本机 APFS 前置实验，04 §6.9.3 §3.0）：稀疏洞不计（收益主项）、
// 预分配计入、块对齐可大于逻辑大小；唯一看不见的是 CoW 克隆/块级去重的
// 共享 extent（各自报满额），那一类按高估登记，不在本口径内修。
func Reported(path string, info os.FileInfo) (uint64, bool) {
	if info == nil {
		return 0, false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	if st.Blocks < 0 {
		return 0, false
	}
	return uint64(st.Blocks) * BlockSize, true
}

// VolumeID unix：st_dev——同一挂载实例上的文件恒同值，换一个挂载实例
// （哪怕同一个文件系统类型、同一个 statfs f_type）就是另一个值。
// 这正是 M28 要的粒度：卷级证据只在本卷内成立（设计稿 §11.0 E3）。
func VolumeID(path string, info os.FileInfo) (uint64, bool) {
	if info == nil {
		return 0, false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(st.Dev), true
}
