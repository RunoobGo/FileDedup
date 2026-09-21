//go:build !darwin && !linux && !windows

package realbytes

import "os"

// 其他 unix 变体（freebsd 等）：Stat_t 字段布局不通用，一律报"读不到"。
// 与 internal/fsid/fsid_other.go 同一处置——宁可数字退回逻辑口径，
// 也不按猜的字段偏移读一个看似合理的数。
func reported(path string, info os.FileInfo) (uint64, bool) {
	return 0, false
}
