// Package fsid 提取文件的物理身份（设备号/inode/ctime），用于：
//   - 哈希缓存命中判定（防止 size+mtime 一致但内容已变的文件沿用旧哈希）
//   - 破坏性操作前的"路径仍指向同一 inode"复核（压缩 verify→act 的 TOCTOU 窗口）
package fsid

import "os"

// ID 文件物理身份。Resolved=false 表示平台不提供该信息（Windows/未知），
// 调用方应将比较视为"平凡通过"，安全兜底退回到内容级证据（多点采样 + 全量重算）。
type ID struct {
	Dev      uint64
	Ino      uint64
	CtimeNs  int64
	Resolved bool
}

// FromFileInfo 从 os.FileInfo（fstat/lstat 的产物）提取平台身份。
func FromFileInfo(info os.FileInfo) ID {
	if info == nil {
		return ID{}
	}
	return fromInfo(info)
}

// SameIdentity 两 ID 是否指向同一物理文件（不含 ctime：chmod/xattr 等合法
// 元数据操作会推进 ctime，但文件未被替换）。任一侧未解析时返回 true（无从判断）。
func (a ID) SameIdentity(b ID) bool {
	if !a.Resolved || !b.Resolved {
		return true
	}
	return a.Dev == b.Dev && a.Ino == b.Ino
}
