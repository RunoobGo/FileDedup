//go:build windows

package main

// 快照落位的属主证明——windows 腿（M361，第八轮审查批 2）。
//
// ★ 与 !windows 腿分成两枚带 tag 的文件，是因为这件事**跨平台问不出同一个答案**：
//   Windows 上 fs.FileInfo 没有 POSIX 属主可对照， syscall.Stat_t 那一型也不存在。
//   惯例同 internal/ads 的 probe_windows.go / probe_other.go。

import (
	"io/fs"
)

// provablyOwnedOnThisPlatform 在 Windows 上只主张到「固定名上是常规文件」这一档。
//
// ★ 这不是"已证明属于本应用"，而是"能证明的到此为止"：
//
//	ACL 里能不能读出"创建者等于当前用户"是另一套模型（OWNER_LEFT 由安全描述符表达，
//	Go 的标准 os.Stat 不给出），而本轮没有在 Windows 真机上验证过任何一条相关读数。
//	所以这里**故意不谎称**做了属主比对——判据仍是"常规文件即覆盖"，与改前同形，
//	差别只在这一腿被显式记为**未兑现**（对照 M354：Windows 的模式位也只表达只读位，
//	0600 与 0644 都读回 0666，因此那边的用例断言的是"读数必须仍是 0666"）。
func provablyOwnedOnThisPlatform(st fs.FileInfo) bool {
	return st.Mode().IsRegular()
}
