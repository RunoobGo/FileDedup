package ops

import "fmt"

// 本文件承载"回收站可回收性判定"中**与平台无关的纯逻辑**。
//
// 为什么要把它们抽出来：
//
// trash_windows.go 带 `//go:build windows`，其测试同样只能在 Windows 上跑。
// 而本项目的 CI 主门禁跑在 Linux 上——上一轮的教训正是"只写在 Windows
// 文件里的防线从来没被任何自动化测试执行过"。为了让"静默永久删除"这道
// 防线进入 Linux CI 的可测范围，把可判定的部分下沉到本文件。
//
// 平台相关的部分（GetDriveTypeW / SHQueryRecycleBinW / 注册表）留在
// trash_windows.go，通过依赖注入（recyclabilityProbe）与本文件对接。

// humanSize 把字节数格式化成便于阅读的形式（仅用于错误提示）。
func humanSize(n int64) string {
	const (
		kib = int64(1) << 10
		mib = int64(1) << 20
		gib = int64(1) << 30
		tib = int64(1) << 40
	)
	switch {
	case n >= tib:
		return fmt.Sprintf("%.2f TiB", float64(n)/float64(tib))
	case n >= gib:
		return fmt.Sprintf("%.2f GiB", float64(n)/float64(gib))
	case n >= mib:
		return fmt.Sprintf("%.1f MiB", float64(n)/float64(mib))
	case n >= kib:
		return fmt.Sprintf("%.1f KiB", float64(n)/float64(kib))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// RBState 是"回收站状态"的快照：卷根 → 该卷回收站条目数。
// 拿不到某卷数据的卷不会出现在 map 里。前后两次快照用同一类型。
//
// （曾把前/后拆成 RBBefore / RBAfter 两个类型想表达"不可混用"，
// 但它们的数据结构与用途完全一致，反而让 checkRecycled 的调用点
// 需要来回转换。类型系统在这里没有提供任何真实约束，故合并。）
type RBState map[string]int64

// checkRecycled 判定"SHFileOperation 声称成功"之后，文件是否**确实**
// 进了回收站。这是把"静默永久删除"变成"响亮错误"的核心判据。
//
// 输入全部是纯数据，因此可在任意平台单测：
//
//	stillExists   —— 操作后**仍然存在**的源路径（应为空）
//	before/after  —— 操作前后各卷回收站条目数快照
//
// 判定两条：
//
//	判据 1：源必须已消失。若还在，说明 Shell 根本没处理它（返回成功是假的）。
//	判据 2：每个前后都能查到的卷，其回收站条目数必须**严格增加**。
//	        若文件已消失而条目数没增加，就是静默永久删除。
//
// 判据 2 在卷不可查询时自动跳过（before 里没有该卷 → 不参与）。
func checkRecycled(stillExists []string, before, after RBState) error {
	if len(stillExists) > 0 {
		return fmt.Errorf("操作返回成功，但有 %d 个文件仍存在于原路径，未进入回收站；首个: %s",
			len(stillExists), stillExists[0])
	}
	// 遍历顺序对判据无影响，但为了错误信息**稳定可复现**（测试与用户
	// 看到的"首个卷"一致），按卷根排序后取第一个违约者。
	for _, root := range sortedKeys(before) {
		nBefore := before[root]
		nAfter, ok := after[root]
		if !ok {
			continue // 该卷事后无法查询 → 跳过该卷的判据 2
		}
		if nAfter <= nBefore {
			return fmt.Errorf("检出静默永久删除：%s 上的文件已从原路径消失，"+
				"但该卷回收站条目数未增加（%d → %d）。"+
				"这通常是回收站被策略禁用、或文件超出回收站配额所致。"+
				"文件未被放入回收站，可能已永久删除", root, nBefore, nAfter)
		}
	}
	return nil
}

// sortedKeys 返回 map 的键并按字典序排序（仅用于让错误信息稳定）。
func sortedKeys(m RBState) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// 卷根数量极少（通常 1~3 个），简单插入排序足够且无额外 import。
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// capacityReason 判定单文件是否超过该卷回收站容量上限。
//
//	fileSize  —— 文件大小（字节）
//	capBytes  —— 该卷回收站容量上限（字节）；0 或负表示"未知/不限制"
//
// 返回 "" 表示可放行。纯函数，可在 Linux 上直接测。
//
// 注意 capBytes 的语义必须区分清楚：
//
//	0 / 负数  → **未知或不限制**，放行（交事后复核兜底）
//	正数      → 已知上限，文件大于它即拒绝
//
// 刻意不把 0 解释成"容量为零"：注册表里 MaxCapacity 缺失或为 0 表示
// 用户选了"不将文件移入回收站"以外的默认/无限设置，直接在预检里
// 拒掉会让正常用户删不掉任何文件。
func capacityReason(fileSize, capBytes int64) string {
	if capBytes <= 0 {
		return ""
	}
	if fileSize > capBytes {
		return fmt.Sprintf("单文件 %s 超过该卷回收站容量上限 %s，Shell 会静默永久删除",
			humanSize(fileSize), humanSize(capBytes))
	}
	return ""
}
