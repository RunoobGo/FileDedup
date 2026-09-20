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
	err := verifyRecycled([]string{p}, RBState{})
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
	if err := verifyRecycled([]string{p}, RBState{}); err != nil {
		t.Fatalf("无快照时不应报错（判据 2 应跳过），got %v", err)
	}
}
