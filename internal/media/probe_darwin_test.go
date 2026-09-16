//go:build darwin

package media

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

// 测试用的注入桩
func rotNone(string) Class     { return Unknown }
func rotSSD(string) Class      { return SSD }
func rotSpinning(string) Class { return Rotational }

// G6：网络文件系统必须被识别为 Network，且不进入后续判定（含不调 diskutil）。
func TestClassifyNetwork(t *testing.T) {
	for _, ft := range []string{"smbfs", "nfs", "afpfs", "webdav", "ftp", "cifs", "cddafs"} {
		called := false
		in := probeInput{fsType: ft, mount: "/Volumes/net", device: "/dev/disk9s1"}
		got := in.classify(func(string) bool { return false }, func(string) Class {
			called = true
			return SSD
		})
		if got != Network {
			t.Fatalf("fsType %q 应为 Network，实际 %s", ft, got)
		}
		if called {
			t.Fatalf("fsType %q 为网络卷，不应再探测旋转介质", ft)
		}
	}
}

// G6：内置盘快路径——与启动卷同物理盘时必须判 SSD 且不启动 diskutil。
func TestClassifyInternalShortcut(t *testing.T) {
	called := false
	rot := func(string) Class { called = true; return Rotational }
	in := probeInput{fsType: "apfs", mount: "/System/Volumes/Data", device: "/dev/disk3s5"}
	if got := in.classify(func(string) bool { return true }, rot); got != SSD {
		t.Fatalf("内置盘应为 SSD，实际 %s", got)
	}
	if called {
		t.Fatal("内置盘不应启动 diskutil 子进程")
	}
}

// G6：外置卷必须走真实探测，并透传其结果。
func TestClassifyExternal(t *testing.T) {
	in := probeInput{fsType: "apfs", mount: "/Volumes/ext", device: "/dev/disk7s1"}
	if got := in.classify(func(string) bool { return false }, rotSSD); got != SSD {
		t.Fatalf("外置固态应透传 SSD，实际 %s", got)
	}
	if got := in.classify(func(string) bool { return false }, rotSpinning); got != Rotational {
		t.Fatalf("外置机械盘应透传 Rotational，实际 %s", got)
	}
	// 无 isInternal 能力时不得短路（否则会把外置盘误判成内置固态）
	called := false
	in.classify(nil, func(string) Class { called = true; return Rotational })
	if !called {
		t.Fatal("isInternal 为 nil 时不应短路")
	}
	// 无 rot 能力且非内置 → Unknown（不猜测）
	if got := in.classify(func(string) bool { return false }, nil); got != Unknown {
		t.Fatalf("无法探测时应为 Unknown，实际 %s", got)
	}
}

// G6：字段缺失一律 Unknown，绝不猜测。
func TestClassifyMissingFields(t *testing.T) {
	internal := func(string) bool { return true }
	cases := []probeInput{
		{},                                       // statfs 失败：全空
		{fsType: "apfs"},                         // 无挂载点/设备
		{fsType: "apfs", mount: "/x"},            // 无设备
		{fsType: "apfs", device: "/dev/disk3s5"}, // 无挂载点
		{mount: "/x", device: "/dev/disk3s5"},    // 无类型名
	}
	for i, in := range cases {
		if got := in.classify(internal, rotSpinning); got != Unknown {
			t.Fatalf("case[%d] %+v 应为 Unknown，实际 %s", i, in, got)
		}
	}
}

// G6：设备名 → 物理盘标识。关键边界是「整盘无分区后缀」时不能切坏。
func TestDiskKey(t *testing.T) {
	cases := map[string]string{
		"/dev/disk3s5":    "disk3",
		"/dev/disk3s1s1":  "disk3",
		"/dev/disk3":      "disk3",
		"/dev/disk10s2":   "disk10",
		"/dev/disk10":     "disk10",
		"/dev/disk":       "/dev/disk", // 异常：无盘号，原样返回
		"/dev/mapper/foo": "/dev/mapper/foo",
		"":                "",
	}
	for dev, want := range cases {
		if got := diskKey(dev); got != want {
			t.Fatalf("diskKey(%q) = %q, want %q", dev, got, want)
		}
	}
	// 同一 APFS 容器下的系统卷与数据卷必须归为同一物理盘
	if diskKey("/dev/disk3s1s1") != diskKey("/dev/disk3s5") {
		t.Fatal("同容器卷宗应归为同一物理盘")
	}
	// 不同物理盘不得混淆（disk1 不得匹配 disk10）
	if diskKey("/dev/disk1s1") == diskKey("/dev/disk10s1") {
		t.Fatal("disk1 与 disk10 不应被视为同一盘")
	}
}

// G6：diskutil plist 解析——含 真/假/缺键 三种情形。
func TestParseSolidState(t *testing.T) {
	const head = `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
	<key>DeviceIdentifier</key><string>disk0s2</string>
`
	cases := []struct {
		name  string
		plist string
		want  Class
		ok    bool
	}{
		{"固态", head + "\t<key>SolidState</key><true/>\n</dict></plist>", SSD, true},
		{"机械", head + "\t<key>SolidState</key><false/>\n</dict></plist>", Rotational, true},
		// 磁盘映像 / 合成卷通常没有该键：必须 Unknown，不能默认成机械盘
		{"缺键", head + "\t<key>VolumeName</key><string>x</string>\n</dict></plist>", Unknown, false},
		{"空文本", "", Unknown, false},
		{"键后无标记", head + "\t<key>SolidState</key>\n", Unknown, false},
	}
	for _, c := range cases {
		got, ok := parseSolidState(c.plist)
		if got != c.want || ok != c.ok {
			t.Fatalf("%s: parseSolidState = (%s, %v), want (%s, %v)", c.name, got, ok, c.want, c.ok)
		}
	}
}

// G6：diskutil 失败时退出码为 0，必须靠 plist 内容识别错误。
func TestIsDiskutilError(t *testing.T) {
	// 实测样本（diskutil info -plist /var/folders/.../T，退出码 0）
	errPlist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Error</key>
	<true/>
	<key>ErrorMessage</key>
	<string>Could not find disk: /var/folders/x/T</string>
	<key>ExitCode</key>
	<integer>1</integer>
</dict>
</plist>`
	if !isDiskutilError(errPlist) {
		t.Fatal("错误 plist 未被识别")
	}
	okPlist := `<plist version="1.0"><dict>
	<key>SolidState</key><true/>
	<key>Error</key><false/>
</dict></plist>`
	if isDiskutilError(okPlist) {
		t.Fatal("Error=false 的正常 plist 被误判为错误")
	}
	if isDiskutilError(`<plist version="1.0"><dict><key>SolidState</key><true/></dict></plist>`) {
		t.Fatal("无 Error 键的正常 plist 被误判为错误")
	}
}

// G6：真实探测冒烟——不卡死、返回合法类别、快路径零子进程、结果被缓存。
func TestProbeSmoke(t *testing.T) {
	// 不存在的路径：statfs 失败 → 必须快速返回 Unknown（不应走到子进程）
	done := make(chan Class, 1)
	go func() { done <- Probe("/definitely/not/a/real/path/xyz") }()
	select {
	case got := <-done:
		if got != Unknown {
			t.Fatalf("不存在路径应为 Unknown，实际 %s", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Probe 对不存在路径未能及时返回（可能卡在子进程）")
	}

	// 真实路径：返回合法类别，并打印判定细节便于人工核对
	for _, p := range []string{"/", os.TempDir()} {
		in := statfsInfo(p)
		start := time.Now()
		got := Probe(p)
		elapsed := time.Since(start)
		switch got {
		case Unknown, SSD, Rotational, Network:
		default:
			t.Fatalf("Probe(%q) 返回非法类别 %d", p, got)
		}
		t.Logf("Probe(%q) = %s（fsType=%q mount=%q device=%q internal=%v）耗时 %v",
			p, got, in.fsType, in.mount, in.device, isInternalDevice(in.device), elapsed)
		if again := Probe(p); again != got {
			t.Fatalf("缓存前后结果不一致: %s vs %s", again, got)
		}
	}

	// 快路径回归：与启动卷同盘的路径必须零子进程（diskutil 单次约 90ms）。
	// 此断言失败说明内置盘短路被破坏，扫描启动会平白多出近百毫秒。
	in := statfsInfo("/")
	if isInternalDevice(in.device) {
		start := time.Now()
		if got := Probe("/"); got != SSD {
			t.Fatalf("内置盘应为 SSD（快路径），实际 %s", got)
		}
		if d := time.Since(start); d > 20*time.Millisecond {
			t.Fatalf("内置盘探测耗时 %v，疑似走回了 diskutil 子进程", d)
		}
	}

	if _, err := exec.LookPath("diskutil"); err == nil {
		if got := Probe("/"); got == Unknown {
			t.Fatal("diskutil 可用时 / 不应为 Unknown，探测链可能已失效")
		}
	}
}
