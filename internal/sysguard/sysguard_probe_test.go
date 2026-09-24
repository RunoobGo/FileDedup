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

	"filededup/internal/pathnorm"
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

// TestAbsPathEntryReliesOnConstantBackslashSwap 补强（M64 变异 M-M64-b 暴露的覆盖缺口）。
//
// 现场：内置 eAbsPath 全是 POSIX 形（/proc /sys /dev /run /System，加 darwin 的
// /private/… 八条——M84 起那条整片 /private 已拆成后代子树，见 sysguard.go 表注释），
// 而 Windows 风格的待判路径命中的是 dirName 条目（$Recycle.Bin 一类），压根走不到
// absPath 那条循环。于是"Dir 先把反斜杠恒换成 /"这条语义在本包**没有任何用例需要
// 它成立**——M64 收归时把 Slash 换成 filepath.ToSlash（unix 上即恒等映射）做变异，
// filter 与 scanner 都红了，sysguard 与 ops 却一路绿。
//
// 后果不是纸面的：absPath 是"登记一条就挡一整棵子树"的那一档。下一个登记 Windows
// 形绝对路径的人（外接卷挂载点、`C:/Windows/Temp` 这类）拿到的会是一条
// **永远不命中、且一声不响**的保护规则；在 Linux 主门禁上尤其看不出，
// 因为 CI 就在 unix 上跑。
//
// ★ 故意改全局 table 再装配（同 APP-9 探针的写法）：现有 POSIX 条目测不出这条腿。
// 本包无 t.Parallel（已核），defer 还原，不外溢到同包其它用例。
func TestAbsPathEntryReliesOnConstantBackslashSwap(t *testing.T) {
	defer func(orig []entry) { table = orig }(table)
	table = append(append([]entry{}, table...), entry{
		kind: eAbsPath, plat: pAll, name: "C:/Windows",
		why: "探针条目：Windows 形绝对路径锚定",
	})

	// 非 Windows 宿主上必须照样挡住——判据是纯字符串的，不吃宿主分隔符。
	g := New(PlatformLinux)
	for _, p := range []string{
		`C:\Windows\System32`, // 反斜杠形：只有恒换才成立
		`C:\Windows`,          // 锚定路径本身
		`C:/Windows/Temp`,     // 已归一形：恒换对它无影响
		`C:\Windows\`,         // 尾部斜杠：去尾必须在换分隔符之后仍然保得住匹配
	} {
		if d := g.Dir(p, base(p)); d.Kind != KindProtectedDir {
			t.Errorf("Dir(%q) 未判出受保护（Kind=%v）——absPath 一侧依赖的\"恒换反斜杠\"语义失效了", p, d.Kind)
		}
	}

	// 反面对照一：只差一个分隔符不是后代（钉 Under 的边界，M-M64-c 的第二个靶子）。
	for _, p := range []string{`C:\WindowsExtra\foo`, `C:\Windows.old`} {
		if d := g.Dir(p, base(p)); d.Skip {
			t.Errorf("Dir(%q) 被误挡：absPath 只认该路径本身及其以分隔符界定的子项", p)
		}
	}
	// 反面对照二：换分隔符不得顺手把大小写也放宽（absPath 是有意的精确匹配，
	// 见 table 注释"大小写精确、只认绝对根"）。
	if d := g.Dir(`C:\windows\system32`, "system32"); d.Skip {
		t.Error("absPath 变成大小写不敏感：/System 与镜像根目录里的同名目录会互相误伤")
	}
}

// M216（2026-09-24 第五轮审查批）：包尾注释声称"条目侧统一走 Slash+TrimTailKeepRoot"，
// 改前 New() 只对 prefix/suffix 折小写、eAbsPath.name 原样入桶，归一只发生在查询侧。
// 现读内置 13 条 absPath 全是干净 POSIX 形 ⇒ 今日无实害，但下一条按注释形状登记的
// "/dev/"（尾斜杠）或反斜杠形条目会静默失配 —— latent fail-open：保护清单不命中且
// 毫无征兆，正是这包最怕的那一类缺陷。
//
// ★ 故意改全局 table 再装配（同 APP-9 / 反斜杠恒换探针的写法）；本包无 t.Parallel。
func TestM216AbsPathEntryNormalizedAtAssembly(t *testing.T) {
	// P-216-a（修前真红）：登记"看着最自然"的尾斜杠形，后代必须照样被挡。
	defer func(orig []entry) { table = orig }(table)
	table = append(append([]entry{}, table...), entry{
		kind: eAbsPath, plat: pAll, name: "/audit-probe-dir/", // ★ 故意带尾斜杠
		why: "M216 探针：条目侧尾斜杠必须被 New() 归一",
	})

	g := New(PlatformLinux)
	if d := g.Dir("/audit-probe-dir/sub", base("/audit-probe-dir/sub")); !d.Skip {
		t.Errorf("尾斜杠条目 /audit-probe-dir/ 未挡住其后代（Kind=%v）：条目侧未归一 ⇒ Under 落在不同键空间（改前红）", d.Kind)
	}
	// 归一不能把匹配放宽：只差一个分隔符的邻居不得被误伤（Under 的分隔符边界）。
	if d := g.Dir("/audit-probe-dirX/sub", base("/audit-probe-dirX/sub")); d.Skip {
		t.Error("/audit-probe-dirX 被误挡：归一只去尾斜杠，不放宽匹配语义")
	}
}

// P-216-b（自检锚）：遍历内置表，断言每条 absPath 名**已等于**其归一值——
// 防"注释又跑回实现前面"。现读内置 absPath 13 条全为干净 POSIX 形，本条对现状恒绿，
// 它拦的是"未来有人登记了未归一形状、却又没像 M216 探针那样显式改表"的漂移。
func TestM216BuiltinAbsPathEntriesAreAlreadyNormalized(t *testing.T) {
	var checked int
	for _, e := range table {
		if e.kind != eAbsPath {
			continue
		}
		checked++
		want := pathnorm.TrimTailKeepRoot(pathnorm.Slash(e.name, "\\"))
		if e.name != want {
			t.Errorf("内置 absPath 条目 %q 非归一形（应为 %q）：装配期归一与登记形不符", e.name, want)
		}
	}
	// M146 下界：清单被删空 ⇒ 本条循环 0 次照样绿，等于没跑。现读 13 条，下界 12。
	if checked < 12 {
		t.Fatalf("absPath 条目数 = %d，下界 12：判据清单被删空 ⇒ 本条等于没跑（M146）", checked)
	}
}
