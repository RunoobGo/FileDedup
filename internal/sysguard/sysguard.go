// Package sysguard 内置「系统目录 / 伪文件 / 保留名」保护清单（M6-P4，2026-09-21）。
//
// 为什么必须引擎内置、而不是交给用户的 ExcludePaths（总纲 §2.4 / 04 §6.7 C 组 4）：
// 现有遍历期只有三道跳过——`.` 前缀隐藏规则、用户 ExcludePaths（前端默认空）、
// 应用自身的 .fdd-* 临时名。这三道都挡不住 Windows 的 $Recycle.Bin（首字符是 $）
// 与 System Volume Information，也挡不住盘根 pagefile.sys。后果不是"噪声"而已：
// 回收站里的副本会被收进语料并和原位文件配成重复组，用户据此再删一次；
// ACL 拒绝的目录则把失败抽屉灌满，真正的读不了被淹没。
//
// 因此这份清单**不可被用户配置关闭**（Settings 里没有对应开关，也没有能清空它的入口）。
// 唯一例外是用户显式把受保护路径本身（或其内部）设为扫描根——那是专家通道，
// 由调用方按"根是否落在被剪枝目录内"放行，并把该目录计入 UnprotectedRoots 供界面警示。
//
// 分层约定（04 §6.8.0 约束 5）：本包是**纯字符串判据**，无 build tag、无 syscall、
// 不 import 任何 internal 包；平台差异以 New(platform) 参数注入。因此"Windows 保留名
// 规则""macOS TM 快照规则"这些只在别的系统上成立的东西，在 Linux 主门禁里就是可执行断言。
// 带 tag 的文件只有 platform_*.go 三个，各自唯一的作用是给出 Current 常量。
//
// 判据的作用范围刻意分成四种条目类型（设计稿 §2.2），因为"平铺一张清单"只有两种
// 收场：全按段名匹配（于是 /System 会误伤 D:\System、/mnt/x/system）或引入 glob
// （于是多一套要测的语义）。分型之后每类的适用面是可读死的：
//
//	dirName     目录，任意深度，按**自身段名**，大小写不敏感
//	absPath     目录，按**绝对路径**相等或其后代，大小写精确
//	pseudoFile  文件，仅当父目录恰是扫描根，大小写不敏感
//	reserved    文件与目录，仅 Windows 平台位，大小写不敏感
package sysguard

import "strings"

// Platform 是保护清单的生效面。由三个 platform_*.go 各按 GOOS 给出 Current。
type Platform int

const (
	PlatformLinux Platform = iota
	PlatformDarwin
	PlatformWindows
)

// Kind 说明"为什么没进语料"。三类都必须可见地分开计数与分开文案：
// 混成一格会让用户以为保护清单在吞自己的数据（反之亦然）。
type Kind int

const (
	KindNone Kind = iota
	// KindProtectedDir 系统保护目录被剪枝（整棵子树不下潜，因此只能计目录数）。
	KindProtectedDir
	// KindProtectedFile 盘根伪文件（pagefile.sys 一类）。
	KindProtectedFile
	// KindReservedName Windows 保留设备名（CON/NUL/COM1-9…）。
	KindReservedName
)

// Decision 判定结果。Reason 是给日志与报告看的短句（调用方负责拼路径），
// 命中却给不出理由是缺陷：用户看到"少了 N 个"必须能问到为什么。
type Decision struct {
	Skip   bool
	Kind   Kind
	Reason string
}

// 平台位：条目自带生效面。dirName 类条目一律三平台通吃（见各条理由），
// 真正需要平台位的是 absPath（路径根不同）、pseudoFile 与 reserved（Windows 专属）。
const (
	pLinux uint8 = 1 << iota
	pDarwin
	pWindows
	pAll     = pLinux | pDarwin | pWindows
	pWinOnly = pWindows
)

type entryKind uint8

const (
	eDirName entryKind = iota
	eAbsPath
	ePseudoFile
)

// entry 一条清单登记。prefix/suffix 只用于 eDirName 的"前后缀式"条目
// （TM 本地快照那种带时间戳的名字）；两者皆空即整段名相等。
// prefix/suffix 在 New() 装配时被折成小写，登记时按直觉写大小写即可（APP-9）。
type entry struct {
	kind   entryKind
	plat   uint8
	name   string // eAbsPath 时为归一后的绝对路径
	prefix string
	suffix string
	why    string
}

// hits 名字类判定（eDirName / ePseudoFile 共用）：大小写不敏感。
//
// 前提是登记值已被 New() 折成小写（APP-9）：这里只对**待判名字**做 ToLower，
// 登记值一侧不再比较原文，所以归一必须发生在装配时、且只发生一次。
// 名字一次 ToLower 复用于前缀与后缀两趟——这个函数在遍历期每个目录都要跑，
// 原先逐趟各折一次，等于把同一条热路径上的分配翻了一倍。
func (e entry) hits(name string) bool {
	if e.prefix == "" && e.suffix == "" {
		return strings.EqualFold(name, e.name)
	}
	lower := strings.ToLower(name)
	if len(lower) <= len(e.prefix) || !strings.HasPrefix(lower, e.prefix) {
		return false
	}
	return e.suffix == "" || strings.HasSuffix(lower, e.suffix)
}

// table 是**唯一**的清单来源：新增一条只在这里登记。
// 顺序无关（判定按类型分桶，不短路）。
var table = []entry{
	// ---- 卷元数据目录：三平台通吃 ----
	// 理由不是"三个系统都会产生它"，而是**外接卷跨平台挂载**才是主要受害场景：
	// Linux 挂载一块 NTFS 盘，$Recycle.Bin 与 System Volume Information 都在，
	// 而 ACL 保护在 Linux 上不存在，进去就是真读真扫。
	{kind: eDirName, plat: pAll, name: "$Recycle.Bin", why: "回收站实体目录，里面是已删除待清理的副本"},
	{kind: eDirName, plat: pAll, name: "System Volume Information", why: "卷影复制与系统卷元数据"},
	{kind: eDirName, plat: pAll, name: "lost+found", why: "文件系统修复后的 inode 残骸目录"},
	{kind: eDirName, plat: pAll, name: ".Spotlight-V100", why: "macOS 卷的 Spotlight 索引"},
	{kind: eDirName, plat: pAll, name: ".fseventsd", why: "macOS 卷的文件系统事件日志"},
	{kind: eDirName, plat: pAll, name: ".Trashes", why: "macOS 外接卷的回收站"},
	{kind: eDirName, plat: pAll, name: ".DocumentRevisions-V100", why: "macOS 文档历史版本库"},
	// TM 本地快照名形如 .com.apple.TimeMachine-09-20-2026-010203.snapshots。
	// 用前缀+后缀而不是 glob：整卷历史副本必须挡住，但
	// ".com.apple.TimeMachine-notes.txt" 这类用户文件不能连带误伤。
	{kind: eDirName, plat: pAll, prefix: ".com.apple.timemachine-", suffix: ".snapshots",
		why: "Time Machine 本地快照（删它等于删备份）"},
	{kind: eDirName, plat: pWinOnly, name: "WindowsApps", why: "Store 应用包目录，受保护 ACL"},

	// ---- 绝对路径锚定条目：大小写精确、只认绝对根 ----
	// 放宽成"任意段同名"会误伤真实数据（镜像根目录里的 /system、挂载盘里的 /proc）。
	{kind: eAbsPath, plat: pLinux, name: "/proc", why: "伪文件系统，读到的是内核生成物"},
	{kind: eAbsPath, plat: pLinux, name: "/sys", why: "sysfs 伪文件系统"},
	{kind: eAbsPath, plat: pLinux, name: "/dev", why: "设备节点目录"},
	{kind: eAbsPath, plat: pLinux, name: "/run", why: "运行时伪文件系统"},
	{kind: eAbsPath, plat: pDarwin, name: "/private", why: "var/tmp/folders 等运行时目录的真身"},
	{kind: eAbsPath, plat: pDarwin, name: "/System", why: "系统卷；其下 /System/Volumes/Data 是用户数据卷的挂载点"},

	// ---- 盘根伪文件：仅当父目录恰是扫描根（设计稿 §2.2：不做卷根判定）----
	{kind: ePseudoFile, plat: pWinOnly, name: "pagefile.sys", why: "页面文件，恒被系统占用且内容随时变"},
	{kind: ePseudoFile, plat: pWinOnly, name: "hiberfil.sys", why: "休眠文件，逻辑大小=物理内存"},
	{kind: ePseudoFile, plat: pWinOnly, name: "swapfile.sys", why: "交换文件"},
	{kind: ePseudoFile, plat: pWinOnly, name: "DumpStack.log.tmp", why: "崩溃转储栈文件"},
}

// Guard 按平台编译好的清单，只读复用（对齐 filter.Matcher 的预编译做法）。
type Guard struct {
	dirNames    []entry
	absPaths    []entry
	pseudoFiles []entry
	reserved    bool // 平台是否启用 Windows 保留名规则
	platform    Platform
}

// New 按平台装配清单。未知平台按 Linux 处理（清单最保守的那一档）。
func New(p Platform) *Guard {
	g := &Guard{platform: p}
	var bit uint8
	switch p {
	case PlatformDarwin:
		bit = pDarwin
	case PlatformWindows:
		bit = pWindows
		g.reserved = true
	default:
		bit = pLinux
	}
	for _, e := range table {
		if e.plat&bit == 0 {
			continue
		}
		// 登记值统一折成小写再入桶（APP-9）：hits 把待判名字 ToLower 后与登记值
		// 直接比，登记成 ".com.apple.TimeMachine-" 这种"看着最自然"的大小写就会
		// 让规则永不命中且毫无征兆——保护清单静默失效是最坏的一类缺陷，
		// 所以在装配处兜住，而不是靠注释提醒人肉核对。
		e.prefix = strings.ToLower(e.prefix)
		e.suffix = strings.ToLower(e.suffix)
		switch e.kind {
		case eDirName:
			g.dirNames = append(g.dirNames, e)
		case eAbsPath:
			g.absPaths = append(g.absPaths, e)
		case ePseudoFile:
			g.pseudoFiles = append(g.pseudoFiles, e)
		}
	}
	return g
}

// Platform 报告装配所用的平台（诊断与日志用）。
func (g *Guard) Platform() Platform { return g.platform }

// Dir 判定目录 dirAbs（全路径）/ dirName（末段名）是否受保护、应否剪枝。
//
// 只看**自身那一段**是否命中 dirName 条目，不逐段扫祖先：遍历器在祖先处就把
// 整棵子树剪掉了，后代永远不会被单独问起。逐段匹配会让每个目录多一次 Split，
// 落在本项目最热的路径上换不来任何安全性。
//
// pseudoFile 是文件级判据，不套用于目录；reserved 对目录同样成立
// （名为 CON 的目录在 Win32 同样不可寻址）。
func (g *Guard) Dir(dirAbs, dirName string) Decision {
	p := normalize(dirAbs)
	n := normalize(dirName)
	if p == "" && n == "" {
		return Decision{}
	}
	for _, e := range g.dirNames {
		if n != "" && e.hits(n) {
			return Decision{Skip: true, Kind: KindProtectedDir, Reason: "系统保护目录：" + e.why}
		}
	}
	for _, e := range g.absPaths {
		if p != "" && under(p, e.name) {
			return Decision{Skip: true, Kind: KindProtectedDir, Reason: "系统保护目录：" + e.why}
		}
	}
	if g.reserved && n != "" && isReservedName(n) {
		return Decision{Skip: true, Kind: KindReservedName, Reason: windowsReservedWhy}
	}
	return Decision{}
}

// File 判定文件 fileName（末段名）是否受保护。isScanRootChild = 其父目录恰是
// 本次的某个扫描根（由调用方判定，本包不做任何"卷根"推断）。
//
// 只吃名字、不吃全路径是刻意的：文件级判据（保留名、盘根伪文件）与路径无关，
// 多传一个用不上的参数会让"这条判据到底看了什么"读不出来。
//
// 刻意**不**套用 dirName 与 absPath：那两类是目录判据。用户把 D:\$Recycle.Bin
// 或 /proc/self 显式设为扫描根时走的是逃逸通道，此时里面的文件应当照常参与
// 去重——否则"已脱离系统保护、按你指的根扫"这句话是假的（一个文件都扫不到，
// 结果页显示 0 组且无任何解释）。
func (g *Guard) File(fileName string, isScanRootChild bool) Decision {
	n := normalize(fileName)
	if n == "" {
		return Decision{}
	}
	if g.reserved && isReservedName(n) {
		return Decision{Skip: true, Kind: KindReservedName, Reason: windowsReservedWhy}
	}
	if !isScanRootChild {
		return Decision{}
	}
	for _, e := range g.pseudoFiles {
		if strings.EqualFold(n, e.name) {
			return Decision{Skip: true, Kind: KindProtectedFile, Reason: "系统盘根伪文件：" + e.why}
		}
	}
	return Decision{}
}

// windowsReservedWhy 的清单必须与 isReservedName 的集合逐项对应（APP-7）：
// 少列一项，用户看到"这文件被跳过了"却在本包别处找不到它的登记。
const windowsReservedWhy = "Windows 保留设备名（CON/PRN/AUX/CLOCK$/NUL/COM1-9/LPT1-9），它不是一个文件"

// isReservedName 按 Win32 的名字归一取词干：先剥掉尾随的空格与点（这些在
// Win32 里与不含它们的写法是同一个名字），再取第一个点之前的部分。
// COM0/LPT0 **不是**保留名（保留区间是 1-9），别顺手扩成 0-9。
//
// CLOCK$ 是 MS-DOS 实时时钟设备别名，微软的保留名清单里一直挂着它（APP-7 补）：
// 带 $ 是它的名字本身，不是"扩展名"，所以它落在裸名分支而不是 COM/LPT 那条
// 按位置取字符的分支。
func isReservedName(name string) bool {
	s := strings.ToUpper(strings.TrimRight(name, " ."))
	if i := strings.IndexByte(s, '.'); i >= 0 {
		s = s[:i]
	}
	switch {
	case s == "CON", s == "PRN", s == "AUX", s == "CLOCK$", s == "NUL":
		return true
	}
	// 端口号只有 1-9 是保留名：COM0/LPT0 在 Win32 里不是设备别名，
	// 把它们一并挡掉就是误伤用户文件。
	if len(s) == 4 {
		if t := s[:3]; (t == "COM" || t == "LPT") && s[3] >= '1' && s[3] <= '9' {
			return true
		}
	}
	return false
}

// normalize 统一分隔符并去掉尾部斜杠（根路径 "/" 本身保留）。
// 不用 filepath.ToSlash/Clean：它们在 Linux 上把 "\" 当普通字符，
// 而本包必须在任一 GOOS 上都能判定 Windows 风格路径。
func normalize(p string) string {
	if strings.Contains(p, "\\") {
		p = strings.ReplaceAll(p, "\\", "/")
	}
	for len(p) > 1 && strings.HasSuffix(p, "/") {
		p = p[:len(p)-1]
	}
	return p
}

// under e 是否为 p 本身或其祖先目录（absPath 条目专用，大小写精确）。
func under(p, e string) bool {
	if p == e {
		return true
	}
	return strings.HasPrefix(p, e+"/")
}
