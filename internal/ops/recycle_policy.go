package ops

import (
	"fmt"
	"sort"
	"unicode/utf16"
)

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

// ★ 2026-09-20（ocr 审查 M2/M5）回收站 NukeOnDelete 策略预检。
//
// 修正前的三个洞：
//  1. 只读**全局** HKCU\...\Explorer\BitBucket\NukeOnDelete。用户在回收站
//     属性对话框里对单个数据盘勾「不将文件移入回收站」时，写下的其实是
//     BitBucket\Volume\{GUID}\NukeOnDelete——全局键看不到，预检放行，
//     整批文件被 Shell 永久删除后**才**由事后复核报警：数据已经没了。
//  2. 文档承诺「ok=false 表示无法判定」，实现却永不返回 false：
//     「键/值不存在（正常，未禁用）」与「注册表读不到（ACL/异常）」
//     被混为同一个"未禁用"。
//  3. 状态若混合，拒绝文案分不清是全局还是按卷禁用。
//
// 修复：读取端（trash_windows.go）产出三态 nukeStatus，本文件只做纯决策，
// Linux CI 能跑到判定表。

// nukeStatus 是注册表 NukeOnDelete 的三态读数。
type nukeStatus uint8

const (
	// nukeUnknown 读不到/无法判定（键被 ACL 拒绝、类型不是 REG_DWORD 等）。
	// ★ 刻意**不**据此拒绝：未知≠已知禁用，误拒会让正常用户删不掉任何文件；
	// 该情形由 verifyRecycled 的事后复核兜底——它把"静默"变成"响亮的错误"，
	// 虽然为时已晚，但预检层的职责是拦下**可枚举**的降级路径。
	nukeUnknown nukeStatus = iota
	// nukeOff 键或值确实不存在 = 用户未启用该删除策略（全新账户的常态）。
	nukeOff
	// nukeOn NukeOnDelete != 0：该范围内的删除一律不进回收站。
	nukeOn
)

// volumeNukeKey 拼按卷 NukeOnDelete 的注册表路径（HKEY_CURRENT_USER 下）：
// `...\BitBucket\Volume\{GUID}\NukeOnDelete`。
//
// GUID 必须由 volumeGUID(卷根) 现取——它是路径能否命中的唯一变量，
// 从别处缓存来拼就会在换盘/重格式化后静默失配（按卷预检退化为查不到）。
// 值名也拼在路径里，只是为了让形状可被测试断言；实际查询时
// Windows 侧按 `\` 切回「键 + 值名」两段。
func volumeNukeKey(guid string) string {
	return volumePolicyKey(guid) + `\` + nukeValueName
}

// volumePolicyKey 拼按卷策略**键**（不含值名）：`...\BitBucket\Volume\{GUID}`。
//
// ★ AS-R4 同源问题：NukeOnDelete 与 MaxCapacity 两项预检读的是同一个键，
// 修正前 Windows 侧为 MaxCapacity 又抄了一遍整条前缀字面量。漂移后
// registryOpenKey 报错 → 返回 "未知" → 放行，同样是 fail-open。
// 前缀因此只许由 recycleBinPolicyBase 拼一次。
func volumePolicyKey(guid string) string {
	return recycleBinPolicyBase + `\Volume\` + guid
}

// globalNukeKey 拼**全局** NukeOnDelete 的注册表路径（HKEY_CURRENT_USER 下）。
//
// ★ AS-R4（2026-09-20 全仓审计）：修正前 Windows 侧把这条路径又抄了一遍
// 字面量（连同值名）。两处一旦漂移，winNukeStatusOf 读到的是一个不存在的键
// → regIsNotFound → nukeOff →「确认未禁用」，方向是 **fail-open**：用户开了
// 永久删除策略，预检却说没事，整批文件直接消失。故键的拼接只许有这里一份。
func globalNukeKey() string {
	return recycleBinPolicyBase + `\` + nukeValueName
}

// splitRegPath 把 volumeNukeKey/globalNukeKey 的产物切回「键 + 值名」两段
// （键里带值名只是为了让形状可被 Linux 侧测试断言）。
//
// 放在本文件而非 trash_windows.go：切分约定与拼接约定是**同一份契约的两端**，
// 分处两个 build tag 两侧就没法用一条平台无关的往返测试钉住（AS-R4 需要）。
func splitRegPath(full string) (sub, value string) {
	for i := len(full) - 1; i >= 0; i-- {
		if full[i] == '\\' {
			return full[:i], full[i+1:]
		}
	}
	return full, ""
}

// recycleBinPolicyBase 全局 BitBucket 键（HKEY_CURRENT_USER 下相对路径）。
const recycleBinPolicyBase = `Software\Microsoft\Windows\CurrentVersion\Explorer\BitBucket`

// nukeValueName 是 BitBucket（全局与按卷）下的策略值名。
const nukeValueName = "NukeOnDelete"

// recycleNukeReason 由「全局」与「该卷」两个三态读数得出拒绝原因。
//
// 判定表（"卷"指本次操作所在盘）：
//
//	全局 on   → 拒绝（全局禁用覆盖所有卷，与卷读数无关）
//	卷   on   → 拒绝（属性对话框按盘勾的"不放入回收站"）
//	其余组合   → 放行（off=确认未禁用；unknown=无法判定，交事后复核）
func recycleNukeReason(global, volume nukeStatus) string {
	switch {
	case global == nukeOn:
		return "回收站已被系统策略禁用（NukeOnDelete），此操作会直接永久删除"
	case volume == nukeOn:
		return "该卷的回收站已被单独禁用（Volume\\NukeOnDelete），此操作会直接永久删除"
	}
	return ""
}

// ---------- M265 / M269：Shell 回收站通道的路径长度上限 ----------

// shellPathUnitLimit 是 `SHFileOperationW` 能安全受理的**最长路径（UTF-16 码元）**。
//
// ★ 单位是码元，不是字节、不是 rune（M269 立的就是这条量纲错）：真机第一批用 Go 的
// `len()`（字节）记梯度，得到"355 才失效"的读数；改用纯 ASCII 语料逐档逼近后边界落在
// **259 码元进站 / 260 码元丢弃**，回头算带中文段名那两组：355 字节＝259 码元进站、
// 356 字节＝260 码元丢弃——与 ASCII 那条线重合。代理对（emoji）再钉一次：130 个 =
// 260 码元必须拒，129 个 = 258 码元必须放行，rune 数在那两点上都是 130/129、毫无区分力。
//
// ★ 与 `LongPathsEnabled` **无关**：`=1` 态重跑九档逐格同读数（§6.45 二·一）。
// 所以这一道预检不许写成"注册表没开才拒"——那会把已经量到的失效面放小。
const shellPathUnitLimit = 259

// pathUnits 数一条路径占多少个 UTF-16 码元。
//
// ★ 不加减任何偏移：`unicode/utf16.Encode` 不追加终止 NUL，所以它的返回长度就是码元数。
// 边界 259/260 本来就是「不含 NUL 的路径长度」与 MAX_PATH(260，含 NUL) 的关系，
// NUL 已经算在常数额度里了；这里再 `-1` 等于把上限推到 260 码元，正中静默删除那一格。
func pathUnits(p string) int { return len(utf16.Encode([]rune(p))) }

// shellPathDropReason 按**码元数**判定这条路径交给 Shell 回收站是否安全。
// 返回 "" 放行；非空为拒绝原因（直接呈现给用户）。
func shellPathDropReason(units int) string {
	if units <= shellPathUnitLimit {
		return ""
	}
	return fmt.Sprintf("路径长 %d 个 UTF-16 码元，超过系统回收站通道的上限（%d 码元可正常进站，%d 码元起失效）："+
		"Shell 会返回成功却把文件**静默永久删除**（进站痕迹一条都不会有）。"+
		"请改用「移动」把冗余文件挪走，或先自行把它们复制到短路径后再处理",
		units, shellPathUnitLimit, shellPathUnitLimit+1)
}

// longPathDropReasonFor 是 Windows 预检的入口：给路径，交回拒绝原因。
//
// ★ 单列一层是为了让"调用点用的是哪把尺子"成为可测命题。只测 `shellPathDropReason(int)`
// 的话，调用点写成 `shellPathDropReason(len(p))`（字节）照样全绿——而那正是 M269 的形状。
func longPathDropReasonFor(p string) string {
	return shellPathDropReason(pathUnits(p))
}

// ---------- M270：判据 2 被旁路时的降级复核 ----------

// bypassedVolumes 交出"判据 2 实际上没跑"的那几卷。
//
// 规则：`expected[root] > 0`（本轮确有文件该进站）**且**快照两侧不齐全
// （`checkRecycled` 的两条 `continue`：事前没基准 / 事后查不到，两者都会整卷跳过）。
// 修前这两条 `continue` 就是 M270 的事故本体：`SHQueryRecycleBinW` 本机四卷恒
// `ok=false` ⇒ 判据 2 **每卷都被旁路**，"报成功而复核一声未响"成为常态而非例外。
//
// expected 里没有的卷不入选：没有期待就没有对账，硬塞进降级腿只会造出 M279 那类假警报。
func bypassedVolumes(expected, before, after RBState) []string {
	var out []string
	for root, want := range expected {
		if want <= 0 {
			continue
		}
		if _, hasBefore := before[root]; !hasBefore {
			out = append(out, root)
			continue
		}
		if _, hasAfter := after[root]; !hasAfter {
			out = append(out, root)
		}
	}
	sort.Strings(out)
	return out
}

// checkRecycledByEvidence 是判据 2 的降级腿：拿"逐条 `$I` 进站痕迹"替代"该卷条目数增量"。
//
// 三态必须分开，混成一句就是把 M265 的失明换个地方继续犯：
//
//	proven ≥ 期待        → 通过（而且比原判据更强：每条都有实体落点证据，不是计数差）
//	proven < 期待        → 报"检出静默永久删除"，并报缺几条
//	该卷根本没读到目录    → 报"该卷复核不可用"，**不许**写成数据已丢
//
// 第三态是本函数唯一的"不下结论"出口，但它**必须响亮**：改前的形状是静默 `continue`，
// 用户拿到的是"已移入回收站"的成功提示。这里把它变成一条需要核实的错误，
// 同时把话说准——我们没有证据表明文件没了，只是这台机器给不出证据。
func checkRecycledByEvidence(expected RBState, roots []string, proven RBState, scanned map[string]bool) error {
	for _, root := range roots {
		want := expected[root]
		if want <= 0 {
			continue
		}
		if !scanned[root] {
			return fmt.Errorf("该卷复核不可用：%s 上 %d 个文件已从原路径消失，"+
				"但既读不到该卷回收站的条目数，也枚举不了它的 $Recycle.Bin 目录，"+
				"因此无法证明它们进了回收站。"+
				"请立即打开该卷回收站逐个核实（这通常是回收站被第三方工具接管、或该卷权限受限所致）",
				root, want)
		}
		if got := proven[root]; got < want {
			return fmt.Errorf("检出静默永久删除（逐条对账）：%s 上应有 %d 个文件进入回收站，"+
				"$Recycle.Bin 里只找到 %d 条本轮进站痕迹，缺 %d 条。"+
				"缺失的文件没有进站痕迹，可能已被直接删除，也可能在入站后被清理工具立即清走"+
				"（Windows 的 SHQueryRecycleBinW 在本机不可用，故这里用逐条取证代替条目数增量）",
				root, want, got, want-got)
		}
	}
	return nil
}
