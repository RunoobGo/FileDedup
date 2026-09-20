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
//	expected      —— 各卷**预期入站**的条目数（见下）
//	before/after  —— 操作前后各卷回收站条目数快照
//
// 判定三条：
//
//	判据 1：源必须已消失。若还在，说明 Shell 根本没处理它（返回成功是假的）。
//	判据 2：（各卷）回收站条目数**增量必须 ≥ 该卷预期入站数**。
//	        某卷条目数没增加 → 该卷上"静默永久删除"；
//	        增量不足预期 → 该卷上**部分文件**被静默永久删除。
//	判据 3：（各卷）回收站条目数不得减少——减少意味着有其它进程在清理，
//	        此时"增量"不可信，但也没证据表明我们这批失败了，故只按"不足"处理。
//
// 判据 2 在卷不可查询时自动跳过（before 或 after 里没有该卷 → 不参与）。
//
// ★ 2026-09-20 加固（数据丢失级）：修正前判据 2 只查 `nAfter > nBefore`。
// 那对"整批全丢"有效，但对**部分丢失**无效：同卷批量移入 [50GB 视频, 1MB 文本]
// 而回收站配额 10GB 时，Shell 静默永久删除视频、文本正常入站 → 条目数 +1 > 0
// → 复核**通过**，用户丢了 50GB 文件却收到"已移入回收站"的成功提示。
// 现在改为按卷比对"预期入站数"：expected[`F:\`]=2 而增量只有 1 → 检出。
//
// expected 的口径：每个**源路径**按其所处卷各计 1。调用方按同样规则统计
// （见 trash_windows.go expectedRecycledPerVolume）。某卷不在 expected 里
// 视为该卷预期 0，此时退化为旧行为（只查"不减少"），保持向后兼容。
//
// ★ 同一轮的第二个坑（本函数内）：判据 2 的"卷没有增量"分支**必须有基准**。
// before 里没有该卷意味着"事前查不到基准"，此时 nBefore 取值 0，任何大于 0
// 的条目数都会被算成"有增量"——那是个假阴性（漏检）；而若恰好为 0 又会算成
// "无增量"——那是个假阳性（误报，事后才恢复可查的卷被当成数据丢失）。
// 故拆成两趟：**先**只对有基准的卷做定点比对（有基准才谈得上"缺失几个"），
// **再**对"有基准却零增量"的卷补一条通用告警。两个分支都要求 `before` 有该卷，
// `unionState` 的意义才真正落实——否则它的并集只是徒增一次不可靠的比较。
func checkRecycled(stillExists []string, expected, before, after RBState) error {
	if len(stillExists) > 0 {
		return fmt.Errorf("操作返回成功，但有 %d 个文件仍存在于原路径，未进入回收站；首个: %s",
			len(stillExists), stillExists[0])
	}

	// 第一趟：把"有基准（before 有该卷）且事后可查"的卷筛出来，按卷根字典序
	// 处理，保证错误信息稳定可复现。
	//
	// 条目数减少（增量 < 0）意味着有别的进程在清理回收站，此时"增量"本来就
	// 不可信；但若该卷还预期有文件入站，则我们这批文件依然没有着落，照样要报。
	type probe struct {
		root            string
		nBefore, nAfter int64
		want, got       int64
	}
	var probes []probe
	for _, root := range sortedKeys(unionState(expected, before)) {
		nBefore, ok := before[root]
		if !ok {
			continue // 无事前基准 → 本卷不参与判据 2（理由见上）
		}
		nAfter, ok := after[root]
		if !ok {
			continue // 事后查不到 → 无法判定，跳过
		}
		probes = append(probes, probe{
			root:    root,
			nBefore: nBefore,
			nAfter:  nAfter,
			want:    expected[root],
			got:     nAfter - nBefore,
		})
	}

	// 1) 优先报"预期数已知但增量不足"的卷——这是最确定的静默永久删除证据。
	for _, p := range probes {
		if p.want > 0 && p.got < p.want {
			return fmt.Errorf("检出静默永久删除：%s 上应有 %d 个文件进入回收站，"+
				"实际条目数只增加了 %d（%d → %d），缺失 %d 个。"+
				"这通常是回收站被策略禁用、或部分文件超出回收站配额所致。"+
				"缺失的文件未被放入回收站，可能已永久删除，请立即用数据恢复工具检查该卷",
				p.root, p.want, p.got, p.nBefore, p.nAfter, p.want-p.got)
		}
	}

	// 2) 再报"预期数未知、且该卷条目数一点没长"的卷——退化为旧行为。
	//    这种情况多半是本轮有文件落在该卷、但调用方没能给出预期数
	//    （例如 expectedRecycledPerVolume 未挂载卷根解析器）。
	for _, p := range probes {
		if p.want == 0 && p.got <= 0 {
			return fmt.Errorf("检出静默永久删除：%s 上的文件已从原路径消失，"+
				"但该卷回收站条目数未增加（%d → %d）。"+
				"这通常是回收站被策略禁用、或文件超出回收站配额所致。"+
				"文件未被放入回收站，可能已永久删除", p.root, p.nBefore, p.nAfter)
		}
	}
	return nil
}

// unionState 返回两个 RBState 的键并集（值取左值优先，仅用于取键集）。
func unionState(a, b RBState) RBState {
	out := make(RBState, len(a)+len(b))
	for k := range a {
		out[k] = a[k]
	}
	for k := range b {
		if _, ok := out[k]; !ok {
			out[k] = b[k]
		}
	}
	return out
}

// volRootFn 从路径提取卷根的注入点。
//
// 判定本体（checkRecycled）是平台无关的纯逻辑，但它需要"这些文件分别落在
// 哪个卷"这一平台事实。若直接调 trash_windows.go 里的 driveRoot，本文件就
// 只能在 Windows 上编译，判据的回归测试也就跑不进 Linux CI 主门禁——
// 那正是上一轮"只写在 windows 文件里的防线从没被自动化跑过"的教训。
// 故经此变量注入：Windows 侧在 init 里挂上真实实现，其余平台保持 nil
// （此时 expectedRecycledPerVolume 返回空表，判据 2 退化为"不得减少"）。
var volRootFn func(string) string

// expectedRecycledPerVolume 统计每个卷**预期**有多少个文件进入回收站。
//
// 口径：每个源路径按其所处卷各计 1（无论文件大小）。这是"条目数增量"的
// 下界——若某卷实际增量小于它，说明该卷上至少有一个文件没进回收站。
//
// 拿不到卷根的路径不计入（与 snapshotRecycleBinCounts 同口径）：
// 无法定位卷就无法比对增量，此时该路径只能依赖判据 1（源是否消失）。
func expectedRecycledPerVolume(paths []string) RBState {
	out := RBState{}
	if volRootFn == nil {
		return out
	}
	for _, p := range paths {
		root := volRootFn(p)
		if root == "" {
			continue
		}
		out[root]++
	}
	return out
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
