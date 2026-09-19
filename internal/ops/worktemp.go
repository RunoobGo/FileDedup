package ops

import (
	"os"

	"filededup/internal/worktemp"
)

// 本文件把 internal/worktemp 的「工作临时名」定义转出给 ops 包内使用。
//
// 之所以不在此处直接定义而只做转出：判定需要被 internal/scanner 共享，
// 而 scanner 无法引用 ops（会形成依赖环）。故规则本体下沉到叶子包
// worktemp，这里仅保留 ops 内部顺手的短名。
//
// 详细背景见 internal/worktemp 的包注释与 internal/ops/move.go 中
// HardlinkMerge 的收尾说明（缺陷 6：.fdd-old 残留污染后续扫描）。
const (
	FddTempSuffix  = worktemp.SuffixTmp
	FddOldSuffix   = worktemp.SuffixOld
	FddUndoSuffix  = worktemp.SuffixUndo
	FddRestoreMark = worktemp.MarkRestored
)

// IsWorkTempName 见 worktemp.IsTempName。
func IsWorkTempName(name string) bool { return worktemp.IsTempName(name) }

// cleanupWorkTemp 删除一个工作临时文件，并把「删除失败」如实回报（不静默）。
//
// 旧代码用 `_ = os.Remove(backup)` 直接吞掉错误。删除失败本身不该让合并结果
// 变成失败（链接已经建好了），但**必须留下痕迹**：否则残留的 .fdd-old
// 会在用户下次扫描时冒出来，而用户完全不知道它从哪来（缺陷 6 的现场表现）。
// 返回非 nil 表示确有残留未清掉，调用方应据此提示用户。
func cleanupWorkTemp(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
