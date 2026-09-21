package sysguard

// APP-7（2026-09-21 全量审查）：Windows 保留名清单漏了 `CLOCK$`。
//
// Microsoft 给 Win32 的保留名全集是 CON, PRN, AUX, CLOCK$, NUL, COM1-COM9,
// LPT1-LPT9（"Naming a File or Directory"；CLOCK$ 是 MS-DOS 实时时钟设备别名，
// 沿用至今仍是保留名）。本包的 windowsReservedWhy 文案自己写着
// "CON/PRN/AUX/NUL/COM1-9/LPT1-9"，少列 CLOCK$ 不是笔误，而是判据真的没挡——
// 名为 CLOCK$ 的文件在 Win32 下根本无法正常打开/改名，扫描器却把它当普通文件
// 参与去重并派发操作，于是错误来自设备层而不是本应用，用户看到的失败原因是天书。
//
// 取词干那条腿（`CLOCK$.txt` → `CLOCK$`）已有"取第一个点之前"的逻辑在跑，
// 但值不在集合里，所以必须连同带后缀的形态一起钉：这类"看着像普通文件名、
// 词干却是设备名"的形态正是最容易漏的一档。

import (
	"strings"
	"testing"
)

func TestReservedNameCLOCKIsReservedOnWindows(t *testing.T) {
	hit := []string{
		"CLOCK$",         // 裸名
		"clock$",         // 大小写不敏感
		"ClOcK$.TXT",     // 词干形态：后缀不影响判定
		"CLOCK$.txt.bak", // 词干取到第一个点为止，仍是 CLOCK$
		"CLOCK$ ",        // Win32 剥掉尾随空格，与不带空格是同一个设备名
		"CLOCK$.",        // 尾随点同样被剥掉
	}
	for _, n := range hit {
		if d := New(PlatformWindows).File(n, false); d.Kind != KindReservedName {
			t.Errorf("Windows 保留名 %q 未判出（Kind=%v）——对它派发操作会打到设备", n, d.Kind)
		}
	}
	// 目录同样成立（isReservedName 对目录名一体适用，见 Guard.Dir 注释）。
	if d := New(PlatformWindows).Dir(`D:\CLOCK$`, "CLOCK$"); d.Kind != KindReservedName {
		t.Error("名为 CLOCK$ 的目录未判出保留名")
	}

	// 误伤面：词干不是 CLOCK$ 的相近名字必须照常扫。
	// 尤其 CLOCK（无 $）——它是完全合法的用户文件名，多挡一个就是静默漏扫。
	miss := []string{"CLOCK", "clock.md", "CLOCKSH$", "aCLOCK$", "CLOCK$$.log", "TIMECLOCK$"}
	for _, n := range miss {
		if d := New(PlatformWindows).File(n, false); d.Kind == KindReservedName {
			t.Errorf("%q 不是保留名却被打上保留名——用户文件会被静默漏扫", n)
		}
	}

	// 平台不外溢：Linux/macOS 上 CLOCK$ 是合法文件名。
	for _, p := range []Platform{PlatformLinux, PlatformDarwin} {
		for _, n := range []string{"CLOCK$", "CLOCK$.txt"} {
			if d := New(p).File(n, false); d.Skip {
				t.Errorf("平台 %v 上不该套用 Windows 保留名规则：%q", p, n)
			}
		}
	}
}

// TestWindowsReservedWhyListsCLOCK 文案与判据同源：清单补了 CLOCK$，
// 给用户看的那句理由也必须列出它，否则"这个文件为什么被跳过"仍然说不圆。
func TestWindowsReservedWhyListsCLOCK(t *testing.T) {
	if !strings.Contains(windowsReservedWhy, "CLOCK$") {
		t.Errorf("windowsReservedWhy 没提 CLOCK$：%s", windowsReservedWhy)
	}
}

// APP-9（2026-09-21 全量审查）：前后缀式条目的登记值必须在**装配时**折成小写。
//
// 现场：hits() 把待判名字 ToLower 后与 e.prefix / e.suffix **原样**比较
// （sysguard.go:96-99），也就是说登记值必须自己写成小写才能命中——可这个约束
// 既没写在 entry 的注释里，也没有任何检查兜着。table 眼下那条 TM 快照恰好写的是
// 小写，所以缺陷处于"潜伏"状态：下一个登记前后缀条目的人按直觉写成
// `prefix: ".com.apple.TimeMachine-"`（真实 macOS 名字就是这个大小写），
// 得到的是一条**永远不命中、且一声不响**的保护规则。保护清单静默失效是本包
// 最坏的一类缺陷：用户看到扫描正常完成，没有任何地方告诉他快照没被挡。
//
// 修向选"装配时归一"而不是"注释提醒 + 结构检查"：前者是 fail-safe（写错也挡得住），
// 后者只是 fail-loud（本包没有测试时检查，只有人肉看）。归一顺带让 hits() 里
// 那两次 strings.ToLower(name) 合成一次——它在遍历期每个目录都要跑。
//
// ★ 探针故意**改全局 table**再装配：这条缺陷只在"登记值大小写"这一维度上成立，
// 拿现有那条小写登记去测等于什么都没测。本包无 t.Parallel（已核），
// 用 defer 还原，不外溢到同包其它用例。
func TestPrefixSuffixEntryMatchesRegardlessOfRegisteredCase(t *testing.T) {
	defer func(orig []entry) { table = orig }(table)
	table = append(append([]entry{}, table...), entry{
		kind: eDirName, plat: pAll,
		prefix: ".MixedCase-Snap-", suffix: ".SNAPSHOTS", // ★ 故意按直觉写成混合大小写
		why: "探针条目：登记值大小写不该影响命中",
	})

	g := New(PlatformDarwin)
	for _, n := range []string{
		".MixedCase-Snap-0920.SNAPSHOTS", // 与登记值逐字同形
		".mixedcase-snap-0920.snapshots", // 全小写
		".MIXEDCASE-SNAP-0920.Snapshots", // 全大写混后缀
	} {
		if d := g.Dir("/Volumes/Backup/"+n, n); d.Kind != KindProtectedDir {
			t.Errorf("前后缀条目在名字 %q 上未命中（Kind=%v）：登记值的大小写泄漏进了判据", n, d.Kind)
		}
	}
	// 反面对照：前缀对、后缀不对，仍然不能命中（归一不能把匹配放宽成前缀匹配）。
	const near = ".MixedCase-Snap-0920.backup"
	if d := g.Dir("/Volumes/Backup/"+near, near); d.Skip {
		t.Errorf("后缀不同的名字 %q 被误挡：归一不能放宽匹配语义", near)
	}
}

// TestTimeMachineEntryStillPrunesMixedCaseName 钉住归一之后真实条目行为不变：
// macOS TM 本地快照按任何大小写形态出现都要剪枝（外接卷常是大小写不敏感的
// FAT/exFAT，用户手输的根路径大小写也不受控）。
func TestTimeMachineEntryStillPrunesMixedCaseName(t *testing.T) {
	g := New(PlatformDarwin)
	for _, n := range []string{
		".com.apple.TimeMachine-09-20-2026-010203.snapshots",
		".COM.APPLE.TIMEMACHINE-09-20-2026-010203.SNAPSHOTS",
	} {
		if d := g.Dir("/Volumes/Backup/"+n, n); d.Kind != KindProtectedDir {
			t.Errorf("TM 快照 %q 未剪枝（Kind=%v）", n, d.Kind)
		}
	}
	// 误伤面：同前缀的用户文件不得被挡。
	const user = ".com.apple.TimeMachine-notes.txt"
	if d := g.Dir("/Users/me/"+user, user); d.Skip {
		t.Errorf("用户文件 %q 被 TM 前缀条目误挡", user)
	}
}
