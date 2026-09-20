//go:build windows

package ops

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

// SHFileOperationW + FOF_ALLOWUNDO：移入回收站（批量，双 \0 结尾路径列表）。

const (
	foDelete          = 3
	fofAllowUndo      = 0x40
	fofNoConfirmation = 0x10
	fofSilent         = 0x4
	// fofNoErrorUI：出错也不弹消息框（我们自行判定并报错）。
	// 必须设置：否则 Shell 会弹「文件太大，无法放入回收站，是否永久删除？」，
	// 用户点「是」就变成永久删除，而本应用承诺的是"移入回收站"。
	fofNoErrorUI = 0x400
)

// GetDriveTypeW 返回值（winbase.h）
const (
	driveNoRootDir = 1
	driveRemovable = 2
	driveFixed     = 3
	driveRemote    = 4
	driveCDROM     = 5
	driveRamdisk   = 6
)

type shFileOpStruct struct {
	hwnd                  uintptr
	wFunc                 uint32
	pFrom                 *uint16
	pTo                   *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

var (
	shell32             = syscall.NewLazyDLL("shell32.dll")
	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	procSHFileOperation = shell32.NewProc("SHFileOperationW")
	procGetDriveType    = kernel32.NewProc("GetDriveTypeW")
	procSHQueryRBInfo   = shell32.NewProc("SHQueryRecycleBinW")
	procGetVolumeName   = kernel32.NewProc("GetVolumeNameForVolumeMountPointW")
)

// shQueryRBInfo 对应 Win32 SHQUERYRBINFO（shellapi.h）：
//
//	typedef struct _SHQUERYRBINFO {
//	    DWORD cbSize;        // 偏移 0
//	    __int64 i64Size;     // 偏移 4   ← Pack=4，**不是** 8
//	    __int64 i64NumItems; // 偏移 12
//	} SHQUERYRBINFO;         // 共 20 字节
//
// ★ 这里刻意**不**声明成 Go struct。两个 __int64 字段在 C 里按 4 字节对齐
// 落在偏移 4 和 12；而 Go 的 struct 会按 8 字节对齐把它排到 8 和 16，
// 总长变成 24。cbSize 一旦写错，SHQueryRecycleBinW 直接返回 E_INVALIDARG，
// 预检被**静默绕过**——而绕过预检的后果正是本缺陷要消除的静默永久删除。
// 因此改用 []byte + 手工按固定偏移编解码，把布局钉死。
const (
	shQueryRBInfoSize    = 20 // 4 + 8 + 8
	shQueryRBOffsetSize  = 4  // i64Size 在偏移 4
	shQueryRBOffsetItems = 12 // i64NumItems 在偏移 12
)

// queryRecycleBin 调 SHQueryRecycleBinW 取某卷回收站的「已用字节数 / 条目数」。
//
// pszRootPath 必须是**卷根**形式（如 `F:\`）；传具体子目录会失败。
// 返回 ok=false 表示调用不可用（老系统/接口异常），调用方应保守处理。
func queryRecycleBin(root string) (sizeBytes int64, numItems int64, ok bool) {
	p, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return 0, 0, false
	}
	buf := make([]byte, shQueryRBInfoSize)
	*(*uint32)(unsafe.Pointer(&buf[0])) = shQueryRBInfoSize

	r0, _, _ := procSHQueryRBInfo.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&buf[0])),
	)
	if r0 != 0 { // S_OK == 0
		return 0, 0, false
	}
	sizeBytes = int64(*(*uint64)(unsafe.Pointer(&buf[shQueryRBOffsetSize])))
	numItems = int64(*(*uint64)(unsafe.Pointer(&buf[shQueryRBOffsetItems])))
	return sizeBytes, numItems, true
}

// recycleBinDisabledByPolicy 判断回收站是否被策略整体禁用。
//
// 注册表：HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\BitBucket
// 下 NukeOnDelete=1 时，**所有**删除都不进回收站（等价于永久 Shift+Delete）。
// 该键不存在是正常情况（用户没动过设置），按"未禁用"处理。
//
// 返回 (nukeOnDelete, ok)。ok=false 表示无法判定，调用方应保守处理。
func recycleBinDisabledByPolicy() (bool, bool) {
	k, err := registryOpenKey(
		`Software\Microsoft\Windows\CurrentVersion\Explorer\BitBucket`)
	if err != nil {
		return false, true // 键不存在 = 用户没改过 = 未禁用
	}
	defer registryCloseKey(k)

	v, err := registryGetDWORD(k, "NukeOnDelete")
	if err != nil {
		return false, true // 值不存在 = 未禁用
	}
	return v != 0, true
}

// unsafeDriveReason 判定路径所在卷能否保证「回收站可还原」（H3）。
//
// FOF_ALLOWUNDO 是尽力而为：网络盘（DRIVE_REMOTE）、可移动盘、光碟、RAM
// 盘上没有系统回收站，Shell 会静默永久删除且返回成功——"移入回收站"
// 对用户承诺可还原，实际不可恢复，属于数据丢失级缺陷。现在这些卷直接
// 报错拒绝，由执行器逐项记 Failed 并在提示中建议改用「移动」。
// 残余风险：固定盘单文件超过回收站配额时 Shell 也可能直接删除，
// 配额可编程查询的接口冗杂（IQueryRBInfo/SEP2），暂以文档提示兜底。
func unsafeDriveReason(p string) string {
	if strings.HasPrefix(p, `\\`) {
		return "网络位置（UNC 路径）没有系统回收站"
	}
	if len(p) < 3 || p[1] != ':' || (p[2] != '\\' && p[2] != '/') {
		return "无法识别所在卷"
	}
	// GetDriveTypeW 要求盘根路径，且**必须以反斜杠结尾**（MSDN：
	// "A trailing backslash is required."）。此处必须拼出 `F:\`。
	//
	// 修正前写作 p[:2] + `\)`：Go 的**反引号 raw string 不做转义**，
	// 该字面量是「反斜杠 + 右括号」两个字符，拼出的实参是 `F:\)`——
	// 不是合法盘根，GetDriveTypeW 返回 DRIVE_NO_ROOT_DIR(1)，
	// 于是**所有**盘符都被判成「根目录不存在」而拒绝回收站操作。
	root, err := syscall.UTF16PtrFromString(p[:2] + `\`)
	if err != nil {
		return "路径编码失败"
	}
	r, _, _ := procGetDriveType.Call(uintptr(unsafe.Pointer(root)))
	switch int(r) {
	case driveFixed:
		return "" // 有回收站，可撤销
	case driveRemote:
		return "网络驱动器没有系统回收站"
	case driveRemovable:
		return "可移动磁盘没有系统回收站"
	case driveCDROM:
		return "光碟为只读介质"
	case driveRamdisk:
		return "RAM 磁盘无回收站"
	case driveNoRootDir:
		return "根目录不存在"
	default:
		return fmt.Sprintf("卷类型不可回收（GetDriveType=%d）", r)
	}
}

// defaultTrash Windows 无公开 API 可枚举 SHFileOperation 的回收站落位
// （hNameMappings 需额外 COM 释放处理），恒返回空映射；
// 回撤以「打开系统回收站」引导代替（spec §7）。
//
// ★ 2026-09-20 缺陷修复：「移入回收站」后文件被静默永久删除。
//
// FOF_ALLOWUNDO 是**尽力而为**，不是保证。当文件无法进入回收站时
// （回收站被策略禁用、单文件超出回收站配额、该卷 $Recycle.Bin 不可用），
// Shell 的行为是：**永久删除该文件，然后返回 0（成功）**。
// 于是原实现看到 r0==0 就把每一项记成"成功移入回收站"，而文件其实
// 已经从磁盘上彻底消失、回收站里什么都没有——数据丢失级缺陷。
//
// MSDN 对此只字未提（它只说 "Setting that flag sends the file to the
// Recycle Bin"，仿佛必然成功）。实测与社区一致确认该降级行为：
// https://stackoverflow.com/questions/21406508/send-file-to-recylebin-big-file-get-permanent-delete
//
// 修复分两道：
//
//	第一道（事前）——recyclableReason 逐文件预检：卷类型、回收站策略、
//	                单文件是否超出回收站配额。任一不满足即**整批拒绝**。
//	第二道（事后）——SHFileOperation 返回 0 后，再独立复核「源路径确实
//	                消失」且「该卷回收站条目数增加了」。复核不过则报错，
//	                由执行器回退逐文件重试并把真实结果如实落账。
//
// 两道缺一不可：事前预检覆盖不了所有降级路径（例如配额在调用瞬间被
// 其它进程占满），事后复核才是最后一道兜底——它把"静默"变成"响亮的错误"。
func defaultTrash(paths []string) (map[string]string, error) {
	dst := map[string]string{}
	if len(paths) == 0 {
		return dst, nil
	}
	// 先全量预检再动手：任一文件不可回收则整批拒绝。
	// 执行器的批量失败回退（C6）会逐个重试，可回收文件逐项成功、
	// 不可回收文件逐项 Failed，与直接逐项分发的结果等价且不会半途丢失。
	var rejected []string
	for _, p := range paths {
		if why := recyclableReason(p); why != "" {
			rejected = append(rejected, fmt.Sprintf("%s（%s）", p, why))
		}
	}
	if len(rejected) > 0 {
		return dst, fmt.Errorf("%d 个文件无法保证进入回收站，已拒绝以防静默永久删除；请改用「移动」或自行确认后再「永久删除」。首个: %s",
			len(rejected), rejected[0])
	}
	// 注意：SHFileOperation 不支持 \\?\ 前缀，使用普通路径
	from := buildPathList(paths)

	// 事后复核基准：记录操作前每个受影响卷的回收站条目数，
	// 以及每个卷**预期**入站的条目数（供 checkRecycled 的判据 2 比对增量）。
	before := snapshotRecycleBinCounts(paths)
	expected := expectedRecycledPerVolume(paths)

	op := shFileOpStruct{
		wFunc:  foDelete,
		pFrom:  &from[0],
		fFlags: fofAllowUndo | fofNoConfirmation | fofSilent | fofNoErrorUI,
	}
	r0, _, _ := procSHFileOperation.Call(uintptr(unsafe.Pointer(&op)))
	if r0 != 0 {
		return dst, fmt.Errorf("SHFileOperation 错误码 %d", r0)
	}
	if op.fAnyOperationsAborted != 0 {
		return dst, fmt.Errorf("操作被系统中止")
	}
	// ★ 关键：r0==0 **不足以**判定文件进了回收站。复核，否则可能是静默永久删除。
	// 同时比对"每卷预期入站数"，否则同卷内**部分**文件被静默删除会被漏检。
	if err := verifyRecycled(paths, expected, before); err != nil {
		return dst, err
	}
	return dst, nil
}

// recyclableReason 判定单个路径能否**保证**进入系统回收站。
//
// 返回 "" 表示可回收；非空为拒绝原因（直接呈现给用户）。
//
// 相较原先的 unsafeDriveReason，本函数在其之上补了两个此前会导致
// 静默永久删除的判据：回收站策略禁用、单文件超出回收站容量。
func recyclableReason(p string) string {
	if why := unsafeDriveReason(p); why != "" {
		return why
	}
	// 策略层：NukeOnDelete=1 时所有删除都不进回收站。
	if nuked, ok := recycleBinDisabledByPolicy(); ok && nuked {
		return "回收站已被系统策略禁用（NukeOnDelete），此操作会直接永久删除"
	}
	// 容量层：单文件 > 该卷回收站配额时，Shell 会静默永久删除。
	// 这里能做的是"尽力"判断——拿不到配额时保守放行，交由事后复核兜底。
	if why := recycleBinCapacityReason(p); why != "" {
		return why
	}
	return ""
}

// recycleBinCapacityReason 判断路径所在卷的回收站容量是否装得下该文件。
//
// 这是本缺陷最可能的触发路径：Windows 默认把回收站上限设为卷容量的 5%
// 左右，而"跨硬盘重复文件"往往是几十 GB 的镜像/视频/安装包——单个文件
// 就超过整个回收站容量，Shell 于是静默永久删除。
//
// 判据来自注册表：
//
//	HKCU\...\Explorer\BitBucket\Volume\{卷GUID}\MaxCapacity  （单位 MB）
//
// 卷 GUID 通过 GetVolumeNameForVolumeMountPointW(`F:\`) 取得。任一环节
// 拿不到就返回 ""（放行），交由 verifyRecycled 的事后复核兜底——
// 宁可漏判也不误拒。
func recycleBinCapacityReason(p string) string {
	size, err := fileSizeOf(p)
	if err != nil {
		// 文件不存在/不可读：交给上游的身份校验与执行器按 S8 语义处理。
		return ""
	}
	root := driveRoot(p)
	if root == "" {
		return ""
	}
	limit, ok := recycleBinCapBytes(root)
	if !ok {
		return "" // 拿不到配额（用户设为"不限制"/键不存在）→ 交事后复核
	}
	// 判定逻辑在 recycle_policy.go（平台无关，Linux CI 可测）。
	return capacityReason(size, limit)
}

// recycleBinCapBytes 取某卷回收站的容量上限（字节）。ok=false 表示未知。
func recycleBinCapBytes(root string) (int64, bool) {
	guid, ok := volumeGUID(root)
	if !ok {
		return 0, false
	}
	k, err := registryOpenKey(
		`Software\Microsoft\Windows\CurrentVersion\Explorer\BitBucket\Volume\` + guid)
	if err != nil {
		return 0, false
	}
	defer registryCloseKey(k)

	mb, err := registryGetDWORD(k, "MaxCapacity")
	if err != nil || mb == 0 {
		return 0, false
	}
	return int64(mb) << 20, true
}

// volumeGUID 返回挂载点的卷 GUID（形如 {xxxxxxxx-xxxx-...}），
// 供拼注册表 BitBucket\Volume\{GUID} 路径。
func volumeGUID(root string) (string, bool) {
	// GetVolumeNameForVolumeMountPointW 要求挂载点**以反斜杠结尾**。
	if !strings.HasSuffix(root, `\`) {
		root += `\`
	}
	in, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return "", false
	}
	buf := make([]uint16, 64) // 卷 GUID 路径形如 \\?\Volume{...}\
	r0, _, _ := procGetVolumeName.Call(
		uintptr(unsafe.Pointer(in)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)
	if r0 == 0 { // BOOL FALSE
		return "", false
	}
	s := syscall.UTF16ToString(buf)
	// s = `\\?\Volume{d1e2...}\` → 取花括号段
	i := strings.IndexByte(s, '{')
	j := strings.IndexByte(s, '}')
	if i < 0 || j <= i {
		return "", false
	}
	return s[i : j+1], true
}

// fileSizeOf 取文件大小（不存在则返回 error）。
func fileSizeOf(p string) (int64, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// snapshotRecycleBinCounts 记录每个受影响卷当前回收站的条目数。
//
// 返回 map[卷根]条目数。拿不到数据的卷不入表——verifyRecycled 会据此
// 跳过对应卷的"条目数增加"判据，退化为只看"源是否消失"。
func snapshotRecycleBinCounts(paths []string) RBState {
	out := RBState{}
	for _, p := range paths {
		root := driveRoot(p)
		if root == "" {
			continue
		}
		if _, ok := out[root]; ok {
			continue
		}
		if _, n, ok := queryRecycleBin(root); ok {
			out[root] = n
		}
	}
	return out
}

// verifyRecycled SHFileOperation 返回 0 之后的独立复核（第二道防线）。
//
// 本函数只负责**采集平台数据**（哪些源还在、各卷回收站条目数），
// 判定逻辑全部委托给 recycle_policy.go 的 checkRecycled——那里是纯函数，
// 可在 Linux 上跑测试，确保这道防线进入 CI 主门禁。
func verifyRecycled(paths []string, expected, before RBState) error {
	// 判据 1 的数据：源是否还在。
	var still []string
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			still = append(still, p)
		}
	}
	// 判据 2 的数据：操作后各卷回收站条目数。
	after := snapshotRecycleBinCounts(paths)
	return checkRecycled(still, expected, before, after)
}

// driveRoot 从路径提取卷根（`F:\`）；无法识别时返回 ""。
func driveRoot(p string) string {
	if len(p) < 3 || p[1] != ':' || (p[2] != '\\' && p[2] != '/') {
		return ""
	}
	return p[:2] + `\`
}

// init 把 Windows 的卷根解析挂到平台无关判定层（见 recycle_policy.volRootFn）。
// 不挂的话 expectedRecycledPerVolume 恒返回空表 → checkRecycled 判据 2
// 退化成"条目不得减少"，部分丢失就检不出来了。
func init() { volRootFn = driveRoot }

// buildPathList 构造 SHFileOperation 的 pFrom：**以单个 \0 分隔的路径列表，
// 末尾再补一个 \0**（即整体双 \0 结尾）。
//
// 2026-09-19 修复：原实现每个路径额外补一个 \0（`append(from, 0)`），
// 而 syscall.StringToUTF16 **本身就已带终止 NUL**（其实现末尾为
// `return append(buf, 0), nil`）。于是产生连续两个 \0：
//
//	缺陷缓冲区: ["F:\dup\a1.bin",0, 0, "F:\dup\sub\a2.bin",0, 0, "F:\dup\a3.bin",0, 0, 0]
//	                └─ 段0 ─┘  └空段┘
//
// SHFileOperation 把列表解析到**第一个空段**为止，因此它只看到第 1 个路径，
// 其余全被丢弃 —— 用户勾选 N 个文件，**实际只有 1 个进了回收站**，
// 而返回值是 0（成功），界面上看不出任何异常。
//
// 单文件场景恰好正确（第一段就是那个路径），所以此前一直没被发现。
//
// 对照 winapi 的既有实现（WGo/rsrc 等）与 SHFILEOPSTRUCT 文档，
// 正确构造是「每个路径各含自己的终止 NUL，最后再补 1 个 NUL」。
func buildPathList(paths []string) []uint16 {
	n := 0
	for _, p := range paths {
		n += len(utf16FromString(p)) // 已含每段自己的终止 NUL
	}
	buf := make([]uint16, 0, n+1)
	for _, p := range paths {
		buf = append(buf, utf16FromString(p)...)
	}
	return append(buf, 0) // 列表终结符：与上一个 NUL 构成双 \0 结尾
}

func utf16FromString(s string) []uint16 {
	return syscall.StringToUTF16(s)
}
