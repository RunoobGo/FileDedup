//go:build !windows

package main

// 快照落位的属主证明——unix 腿（M361，第八轮审查批 2）。
//
// ★ 与 windows 腿分成两枚带 tag 的文件，是因为这件事**跨平台问不出同一个答案**：
//   那条腿的 os.Stat 不表达属主（见 app_snapshot_owner_windows.go）。惯例同 internal/ads
//   的 probe_windows.go / probe_other.go 与 internal/fsid 的平台三件套。

import (
	"io/fs"
	"os"
	"syscall"
)

// provablyOwnedOnThisPlatform 三件都成立才把固定名上那份认领成本应用的上一份影像：
//
//  1. Lstat 看到的是**常规文件**。符号链接在这里不被穿透——链接自身指向谁是另一件事，
//     把快照改名进链接位上等于按别人选定的目的地写入（os.Rename 替换的是链接本身，
//     而这正是"承诺落点与实际对象脱钩"的那一档）。
//  2. 属主 uid 等于当前进程 euid。
//
// ★ 模式位**不参与**认领：改前留下的旧快照是 0644（本轮实测），拿档位当凭据会把用户
// 已有的一份影像挤成孤立的 aside 文件，那是本批新造的乱，不是防住的害。
func provablyOwnedOnThisPlatform(st fs.FileInfo) bool {
	if !st.Mode().IsRegular() {
		return false
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	return sys.Uid == uint32(os.Geteuid())
}
