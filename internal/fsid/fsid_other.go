//go:build !darwin && !linux && !windows

package fsid

import "os"

// 其他 unix 变体（freebsd 等）：Stat_t 字段布局不通用，保守返回未解析。
func fromInfo(info os.FileInfo) ID { return ID{} }
