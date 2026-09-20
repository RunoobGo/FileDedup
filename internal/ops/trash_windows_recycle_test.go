//go:build windows

package ops

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// 2026-09-20 缺陷回归（Windows 侧）：跨盘「移入回收站」被静默永久删除。
//
// 本文件覆盖 trash_windows.go 中**依赖真实 Win32 调用**的部分；
// 平台无关的判定逻辑在 recycle_policy_test.go（Linux CI 也会跑）。

// TestDriveRoot 卷根提取：只接受 `X:\` 形状。
func TestDriveRoot(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`F:\dup\a.bin`, `F:\`},
		{`F:/dup/a.bin`, `F:\`},
		{`C:\a`, `C:\`},
		{`\\server\share\a.bin`, ""}, // UNC 无盘符
		{`relative\a.bin`, ""},
		{`a.bin`, ""},
		{`F:a.bin`, ""}, // 盘符后无分隔符
		{`F:`, ""},
		{``, ""},
	}
	for _, c := range cases {
		if got := driveRoot(c.in); got != c.want {
			t.Errorf("driveRoot(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestVolumeGUIDShape 卷 GUID 必须是 `{xxxxxxxx-...}` 形状，
// 否则拼出的注册表路径必然打不开，配额预检静默失效。
func TestVolumeGUIDShape(t *testing.T) {
	sysDrive, ok := syscall.Getenv("SystemDrive")
	if !ok || sysDrive == "" {
		sysDrive = "C:"
	}
	root := sysDrive + `\`
	guid, ok := volumeGUID(root)
	if !ok {
		t.Skipf("本机无法解析 %s 的卷 GUID（CI 环境可能受限），跳过", root)
	}
	if !strings.HasPrefix(guid, "{") || !strings.HasSuffix(guid, "}") {
		t.Fatalf("卷 GUID 形状不对: %q（应形如 {xxxxxxxx-xxxx-...}）", guid)
	}
	if len(guid) < 38 { // {8-4-4-4-12} = 38 字符
		t.Fatalf("卷 GUID 过短: %q", guid)
	}
}

// TestRecycleBinCapBytesSystemDrive 系统盘必有回收站配额，
// 且应是正数（典型为卷容量的 5%）。
//
// 该用例同时是**预检本身可用性**的证明：若注册表路径或卷 GUID 解析写错，
// 这里会拿不到配额（ok=false），从而暴露"预检其实没生效"。
//
// 关于 Skip 的取舍：标准 Windows 桌面上系统盘的 MaxCapacity 由 Explorer
// 在首次打开回收站属性时写入，全新账户下**可能确实不存在**。因此这里
// 允许 Skip，但**先把"卷 GUID 能解析出来"这一半能力断言死**——因为
// GUID 解析失败一定是我们代码的问题，与环境无关。这样即便 Skip 了，
// 也已经确保"注册表路径的前半段"是对的。
func TestRecycleBinCapBytesSystemDrive(t *testing.T) {
	sysDrive, ok := syscall.Getenv("SystemDrive")
	if !ok || sysDrive == "" {
		sysDrive = "C:"
	}
	root := sysDrive + `\`

	// 第一半：卷 GUID 必须能解析（环境无关，失败即代码缺陷）。
	if _, ok := volumeGUID(root); !ok {
		t.Fatalf("无法解析 %s 的卷 GUID —— BitBucket 注册表路径必然拼不出来，"+
			"回收站配额预检全程失效（不会报错，只会静默放行）", root)
	}

	// 第二半：配额读数。允许因"用户从未打开过回收站属性"而缺失。
	cap, ok := recycleBinCapBytes(root)
	if !ok {
		t.Skipf("%s 的 MaxCapacity 未写入注册表（全新账户的常见情况），跳过。\n"+
			"注意：这只说明本机没设过配额，**不代表预检代码有错**——\n"+
			"卷 GUID 解析已在上一步断言通过，配额缺失时预检退化为放行，"+
			"由事后复核（verifyRecycled）兜底", sysDrive)
	}
	if cap <= 0 {
		t.Fatalf("%s 回收站配额应为正数，got %d", sysDrive, cap)
	}
	t.Logf("%s 回收站配额 = %s", sysDrive, humanSize(cap))
}

// TestQueryRecycleBinSystemDrive SHQueryRecycleBinW 的调用契约。
//
// 重点验证**结构体布局**：cbSize 必须等于 20（Pack=4）。
// 若误按 Go 的 8 字节对齐写成 24，Shell 返回 E_INVALIDARG，
// 事后复核的判据 2 会被静默跳过——那正是本缺陷要恢复的警报。
func TestQueryRecycleBinSystemDrive(t *testing.T) {
	if shQueryRBInfoSize != 20 {
		t.Fatalf("SHQUERYRBINFO 大小 = %d, want 20（DWORD + 两个 __int64，Pack=4）。"+
			"写错会导致 SHQueryRecycleBinW 返回 E_INVALIDARG，事后复核静默失效",
			shQueryRBInfoSize)
	}
	sysDrive, ok := syscall.Getenv("SystemDrive")
	if !ok || sysDrive == "" {
		sysDrive = "C:"
	}
	root := sysDrive + `\`
	_, n, ok := queryRecycleBin(root)
	if !ok {
		t.Skipf("SHQueryRecycleBinW(%s) 调用失败（CI 沙箱可能禁用了 Shell API），跳过", root)
	}
	if n < 0 {
		t.Fatalf("回收站条目数不应为负: %d", n)
	}
	t.Logf("%s 回收站当前条目数 = %d", root, n)
}

// TestRecyclableReasonFixedDriveAllowed 系统盘必须判为可回收
// （防止本次改动把安全闸门整体收紧，导致正常盘也删不掉文件）。
func TestRecyclableReasonFixedDriveAllowed(t *testing.T) {
	sysDrive, ok := syscall.Getenv("SystemDrive")
	if !ok || sysDrive == "" {
		sysDrive = "C:"
	}
	// 构造一个"就位于系统盘"的假想路径（不要求文件存在）。
	p := sysDrive + `\Windows\Temp\fdd-recyclable-probe.bin`
	why := recyclableReason(p)
	// 允许因"回收站被策略禁用"而拒绝（本机确实禁用了就是正确行为），
	// 但不允许因卷类型误判而拒绝。
	if why != "" && strings.Contains(why, "没有系统回收站") {
		t.Fatalf("系统盘（%s）被误判为「无回收站」: %q\n"+
			"这通常说明 GetDriveTypeW 的盘根构造写错了", sysDrive, why)
	}
}

// TestRecyclableReasonNonFixedStillRejected 本次改动不得放松
// 原有的非固定盘拒绝（那是上一轮缺陷的修复成果）。
func TestRecyclableReasonNonFixedStillRejected(t *testing.T) {
	cases := []struct{ name, path, want string }{
		{"UNC", `\\server\share\a.bin`, "网络位置"},
		{"相对路径", `relative\a.bin`, "无法识别所在卷"},
		{"无盘符", `a.bin`, "无法识别所在卷"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			why := recyclableReason(c.path)
			if why == "" {
				t.Fatalf("%q 应被拒绝", c.path)
			}
			if !strings.Contains(why, c.want) {
				t.Fatalf("拒绝原因 %q 未含 %q", why, c.want)
			}
		})
	}
}

// TestSnapshotRecycleBinCountsIgnoresBadPaths 无法识别的路径不得入表
// （否则事后复核会因为"查不到的卷"而误报）。
func TestSnapshotRecycleBinCountsIgnoresBadPaths(t *testing.T) {
	got := snapshotRecycleBinCounts([]string{`relative\a.bin`, `a.bin`, `\\s\s\a.bin`})
	if len(got) != 0 {
		t.Fatalf("非法路径不应产生回收站快照，got %v", got)
	}
}

// TestVerifyRecycledDetectsExtantSource 源仍在时必须报错
// （"SHFileOperation 返回成功但文件还在"是不一致状态）。
func TestVerifyRecycledDetectsExtantSource(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "still-here.bin")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 文件存在 + 空快照（无卷信息可查）
	err := verifyRecycled([]string{p}, expectedRecycledPerVolume([]string{p}), RBState{})
	if err == nil {
		t.Fatalf("源仍存在却未报错——「返回成功但没删掉」会被漏过")
	}
	if !strings.Contains(err.Error(), "仍存在于原路径") {
		t.Fatalf("错误信息不符: %v", err)
	}
}

// TestVerifyRecycledGoneSourceNoSnapshotOK 源已消失且无卷快照可查时，
// 判据 2 跳过 → 不报错（保守：宁缺勿错判）。
func TestVerifyRecycledGoneSourceNoSnapshotOK(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "already-gone.bin") // 从未创建
	if err := verifyRecycled([]string{p}, expectedRecycledPerVolume([]string{p}), RBState{}); err != nil {
		t.Fatalf("无快照时不应报错（判据 2 应跳过），got %v", err)
	}
}

// ★ 2026-09-20（ocr M2/M5）：以下覆盖 Windows 侧的 NukeOnDelete 读取。
// 判定表本身在 recycle_policy_test.go（Linux CI 主门禁）钉死；
// 这里验证的是「读取端能否把三种状态区分出来」——修正前读不到与不存在
// 都返回"未禁用"，按卷键更是从未查过。

// TestWinNukeStatusOfDistinguishesAbsentFromUnreadable 「键/值不存在」与
// 「打开被拒」必须给出不同状态：前者是正常情形（未设置=未禁用），
// 后者是判定失败（未知）。修正前两者都坍缩成"未禁用"。
func TestWinNukeStatusOfDistinguishesAbsentFromUnreadable(t *testing.T) {
	// 必然存在、但绝无 NukeOnDelete 值的键 → off（确实读到了"没设"）
	k, err := registryOpenKey(`Software\Microsoft\Windows\CurrentVersion\Explorer`)
	if err != nil {
		t.Skipf("本机连 Explorer 键都打不开（%v），注册表不可用，跳过", err)
	}
	registryCloseKey(k)
	if got := winNukeStatusOf(`Software\Microsoft\Windows\CurrentVersion\Explorer`, "NukeOnDelete"); got != nukeOff {
		t.Fatalf("Explorer 键下无 NukeOnDelete 值，应判 off（未禁用），got %v", got)
	}

	// 必然不存在的深层键 → unknown（无法判定）。它可能被父键的 ACL 挡住，
	// 那种情形"未知"恰恰是正确答案——不能当作"未禁用"。
	ghost := `Software\Microsoft\Windows\CurrentVersion\Explorer\BitBucket\Volume\{00000000-0000-0000-0000-000000000000}`
	if got := winNukeStatusOf(ghost, "NukeOnDelete"); got == nukeOn {
		t.Fatalf("不存在的键不可能判出「策略已禁用」，got %v", got)
	}
}

// TestRecycleBinVolumeNukeSystemDrive 系统盘的按卷状态必须**真的去查**
// 注册表：修正前按卷键从未参与判定。GUID 解析失败属于代码缺陷
// （与 TestVolumeGUIDShape 同口径，环境无关）。
func TestRecycleBinVolumeNukeSystemDrive(t *testing.T) {
	sysDrive, ok := syscall.Getenv("SystemDrive")
	if !ok || sysDrive == "" {
		sysDrive = "C:"
	}
	root := strings.ToUpper(sysDrive[:1]) + `:\`
	guid, ok := volumeGUID(root)
	if !ok {
		t.Fatalf("无法解析 %s 的卷 GUID——按卷 NukeOnDelete 与配额预检全程失效", root)
	}
	if !strings.HasPrefix(guid, "{") || !strings.HasSuffix(guid, "}") {
		t.Fatalf("卷 GUID 形状不对: %q", guid)
	}
	switch got := recycleBinVolumeNuke(root); got {
	case nukeOff, nukeUnknown, nukeOn:
		t.Logf("%s 按卷 NukeOnDelete = %v", root, got)
	default:
		t.Fatalf("非法状态 %v", got)
	}
}

// TestRegistryGetDWORDRejectsWrongType M6：HKCU 用户可写，同名 REG_SZ
// （如 "1"，恰好容得下 4 字节）此前会被当作 DWORD 读出 0x31 而判成"已禁用"，
// 或其它字符串误判"未禁用"。类型必须校验。
func TestRegistryGetDWORDRejectsWrongType(t *testing.T) {
	const sub = `Software\qoder-fdd-test-key`
	const name = "StringValue"
	k, disp, err := registryCreateKey(sub)
	if err != nil {
		t.Skipf("无法创建测试键（%v），跳过", err)
	}
	defer registryDeleteKey(sub)
	if disp == regValueExists {
		t.Skip("测试键已存在，跳过")
	}
	if err := registrySetString(k, name, "1"); err != nil {
		t.Fatalf("写入测试 REG_SZ 失败: %v", err)
	}
	registryCloseKey(k)

	k2, err := registryOpenKey(sub)
	if err != nil {
		t.Fatalf("重开测试键失败: %v", err)
	}
	defer registryCloseKey(k2)
	if v, err := registryGetDWORD(k2, name); err == nil {
		t.Fatalf("同名 REG_SZ %q 被当作 DWORD 读出 %d——NukeOnDelete 判定会跟着出错，"+
			"必须校验类型", name, v)
	}
}
