//go:build !windows

package ops

import "os"

// createSymlink 在 linkPath 处创建指向 target 的文件符号链接。
//
// unix 上没有任何特权要求，也不受"同卷"限制——跨卷、跨文件系统均可，
// 这正是它在跨卷去重里替代硬链接的唯一理由（硬链接无法跨卷）。
//
// target 传**路径字符串**而非 inode：符号链接保存的就是这个字符串。
// 因此调用方传什么，日后就按什么解析；本应用的调用方传的是保留项的
// 绝对路径（见 symlink.go:SymlinkMerge），链接因此在保留项被移动后失效
// ——这是软链接的固有语义，也正是 UI 必须提示悬空风险的原因。
func createSymlink(target, linkPath string) error {
	return os.Symlink(target, linkPath)
}

// symlinkTarget 读取链接中保存的目标路径字符串（不解析、不要求目标存在）。
//
// 与 fsid.FromPath 的分工：本函数只回答"链接里写着什么"，
// 不回答"那个位置现在是谁的数据"。前者用于向用户展示与自检，
// 后者才是身份判定。
func symlinkTarget(linkPath string) (string, error) {
	return os.Readlink(linkPath)
}
