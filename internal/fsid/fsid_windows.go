//go:build windows

package fsid

import "os"

// fromInfo Windows：syscall.Stat_t 的 Dev/Ino 由 emulated 值填充，跨重命名
// 不稳定，不足以支撑身份判定。返回未解析，安全兜底退回到内容级证据
// （多点采样比对 + 操作前全量重算，见 verify.go）。
func fromInfo(info os.FileInfo) ID { return ID{} }
