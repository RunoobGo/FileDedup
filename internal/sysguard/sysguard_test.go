package sysguard

// M6-P4（2026-09-21，总纲 §2.4 / 04 §6.7 C 组 4）保护清单判据的行为探针。
//
// 全部用例**显式传平台**，一个都不看宿主 GOOS——这正是把判据做成纯字符串
// 函数的目的：Windows 的保留名规则、macOS 的 TM 快照规则都能在 Linux 主门禁
// 上被真实执行（分层依据见 04 §6.8.0 约束 5 / 设计稿 §1.1）。

import (
	"strings"
	"testing"
)

// base 取路径末段（同时认 "/" 与 "\"），仅供用例把整路径喂给 Dir/File 时补 name。
// 不用 filepath.ToSlash：本包必须在 Linux 门禁上判定 Windows 风格路径，
// 而 ToSlash 在非 Windows 上是恒等映射（"\" 是合法文件名字符）。
func base(p string) string {
	q := strings.ReplaceAll(p, "\\", "/")
	if i := strings.LastIndex(q, "/"); i >= 0 {
		return q[i+1:]
	}
	return q
}

func dirSkip(g *Guard, absPath string) bool {
	return g.Dir(absPath, base(absPath)).Skip
}

func TestGuardRecycleBinDirNameIsCaseInsensitive(t *testing.T) {
	g := New(PlatformWindows)
	// 这些写法在 Win32 上是**同一个对象**（卷根回收站/系统卷信息目录）。
	// 判据一旦做成大小写敏感，$RECYCLE.BIN 就会漏保护——回收站内容进语料。
	for _, n := range []string{
		`D:\$Recycle.Bin`,
		`D:\$RECYCLE.BIN`,
		`C:\System Volume Information`,
		`C:\SYSTEM VOLUME INFORMATION`,
		`C:\Program Files\WindowsApps`,
		`E:\windowsapps`,
	} {
		if !dirSkip(g, n) {
			t.Errorf("Dir(%q) 应判为受保护目录（未跳过）", n)
		}
	}
	// 反向：普通用户目录一个都不许被吞。
	for _, n := range []string{
		`D:\Photos\2026`,
		`C:\Program Files\MyApp`,
		`D:\recycle-tools\bin`, // 含子串但不是同名段
		`D:\volume info`,
	} {
		if dirSkip(g, n) {
			t.Errorf("Dir(%q) 不该受保护（用户数据会被静默漏扫）", n)
		}
	}
	// dirName 判据只看**自身那一段**，不看祖先：遍历器在祖先处已经剪掉了整棵
	// 子树，后代永远不会被单独问起。这里显式钉住该语义，防止有人"顺手"改成
	// 逐段扫描（那是每个目录一次 Split，落在最热的遍历路径上）。
	// "祖先剪枝后后代确实进不来"由 scanner 侧集成用例负责（T6）。
	if dirSkip(g, `D:\$Recycle.Bin\S-1-5-21-1\foo`) {
		t.Log("后代路径自身被判保护：说明实现改成了逐段匹配，需复核遍历热路径开销")
	}
}

func TestGuardAbsPathIsExactCase(t *testing.T) {
	g := New(PlatformLinux)
	for _, n := range []string{"/proc", "/proc/1/task", "/sys/devices", "/dev", "/run/user/1000"} {
		if !dirSkip(g, n) {
			t.Errorf("Dir(%q) 应判为受保护（伪文件系统）", n)
		}
	}
	// absPath **不**放宽成大小写不敏感，也**不**放宽成"任意路径段同名"：
	// 两者都会误伤真实数据（大小写敏感卷上的 /system 镜像根、挂载盘里的 proc 目录）。
	for _, n := range []string{"/PROC", "/proc-something", "/mnt/x/proc", "/data/dev/snap"} {
		if dirSkip(g, n) {
			t.Errorf("Dir(%q) 不该受保护（absPath 必须精确且只在绝对根上生效）", n)
		}
	}
	// 同一份清单在 Linux 上不得带上 macOS 专属条目：/System 是 macOS 的
	// Firmlink 卷根，Linux 上的 /System 是用户自己放的目录。
	if dirSkip(g, "/System/Volumes/Data") {
		t.Error("/System 在 Linux 平台清单里，不该命中")
	}
	if !dirSkip(New(PlatformDarwin), "/System/Volumes/Data") {
		t.Error("/System 在 macOS 必须命中（否则会走两遍用户数据）")
	}
}

func TestGuardPseudoFileOnlyAtScanRoot(t *testing.T) {
	g := New(PlatformWindows)
	for _, n := range []string{`D:\pagefile.sys`, `C:\hiberfil.sys`, `E:\SWAPFILE.SYS`, `C:\DumpStack.log.tmp`} {
		if d := g.File(base(n), true); !d.Skip {
			t.Errorf("File(%q, 根级) 应跳过", n)
		}
	}
	// 嵌套的同名文件是**用户数据**（虚拟机镜像备份、磁盘镜像里的 pagefile.sys）。
	// 本设计的边界：不保护，照常去重（理由见设计稿 §2.2 第二条）。
	for _, n := range []string{`D:\backup\vm\pagefile.sys`, `C:\images\old\hiberfil.sys`} {
		if d := g.File(base(n), false); d.Skip {
			t.Errorf("File(%q, 非根级) 不该跳过：越界保护会静默漏扫用户数据", n)
		}
	}
	// 根级判定不依赖平台伪文件之外的事实：普通文件即使在根级也不该被吞。
	if d := g.File("report.docx", true); d.Skip {
		t.Errorf("根级普通文件被判跳过：%+v", d)
	}
}

func TestGuardReservedNameWindowsOnly(t *testing.T) {
	hit := []string{"CON", "con.txt", "NUL", "nul.dat", "COM1", "com9.log", "LPT1", "LPT9.zip", "PRN", "AUX", "CON "}
	miss := []string{"CONTRADICTION.md", "notes.txt", "COM0.dat", "LPT0.dat", "CONFIG.SYS", "NULLIFY.docx", "console.log", "auxiliary.py", "a-prn.txt"}
	for _, n := range hit {
		if d := New(PlatformWindows).File(n, false); d.Kind != KindReservedName {
			t.Errorf("Windows 保留名 %q 未判出（Kind=%v）——对它做操作会打到设备", n, d.Kind)
		}
	}
	for _, n := range miss {
		if d := New(PlatformWindows).File(n, false); d.Kind == KindReservedName {
			t.Errorf("%q 不是保留名却被打上保留名——用户文件会被静默漏扫", n)
		}
	}
	// 平台不外溢：Linux/macOS 上这些名字是完全合法的文件名，必须照常参与去重。
	for _, p := range []Platform{PlatformLinux, PlatformDarwin} {
		for _, n := range hit {
			if d := New(p).File(n, false); d.Skip {
				t.Errorf("平台 %v 上不该套用 Windows 保留名规则：%q", p, n)
			}
		}
	}
	// 目录同样适用（名为 CON 的目录在 Win32 同样不可寻址）。
	if d := New(PlatformWindows).Dir(`D:\CON`, "CON"); d.Kind != KindReservedName {
		t.Error("Windows 保留名目录未判出")
	}
}

func TestGuardTMAndDotDirsPrunedRegardlessOfHiddenFlag(t *testing.T) {
	g := New(PlatformDarwin)
	// TM 本地快照：整卷历史副本。不挡的话结果页里"重复文件"全是快照，
	// 用户照着删等于删备份。
	for _, n := range []string{
		"/Volumes/Backup/.com.apple.TimeMachine-09-20-2026-010203.snapshots",
		"/Volumes/Backup/.com.apple.TimeMachine-1-2-26-00-00-00.snapshots",
	} {
		if !dirSkip(g, n) {
			t.Errorf("TM 本地快照 %q 未剪枝", n)
		}
	}
	// 前缀命中但不以 .snapshots 结尾的**用户目录**不得误伤。
	for _, n := range []string{"/Users/x/notes/.com.apple.TimeMachine-notes.txt", "/Users/x/notes"} {
		if dirSkip(g, n) {
			t.Errorf("%q 不该受保护", n)
		}
	}
	for _, n := range []string{"/Volumes/X/.Spotlight-V100", "/.fseventsd", "/Volumes/Y/.Trashes", "/.DocumentRevisions-V100"} {
		if !dirSkip(g, n) {
			t.Errorf("macOS 卷元数据目录 %q 未剪枝", n)
		}
	}
	// lost+found：ext4 修复目录，三平台同名可造，因此也是"扫描根位于保护目录
	// 内部"这条逃逸规则的唯一可移植夹具（scanner 侧 T8）。
	for _, p := range []Platform{PlatformLinux, PlatformDarwin, PlatformWindows} {
		if !dirSkip(New(p), "/mnt/disk/lost+found") {
			t.Errorf("平台 %v 上 lost+found 未剪枝", p)
		}
	}
}

func TestGuardNoEmptyEntriesAndReasonNonEmpty(t *testing.T) {
	// 空段/空名不能把整张清单变成"全命中"：若某条目被意外写成与 "" 相等，
	// 每个文件都会被静默跳过，而结果页显示 0 组——最难查的一类假绿。
	for _, p := range []Platform{PlatformWindows, PlatformDarwin, PlatformLinux} {
		g := New(p)
		for _, abs := range []string{"", "/", `\`, "D:", "D:\\", "a"} {
			if d := g.Dir(abs, base(abs)); d.Skip {
				t.Errorf("平台 %v：空/畸形路径 %q 被判跳过：%+v", p, abs, d)
			}
			if d := g.File(base(abs), true); d.Skip {
				t.Errorf("平台 %v：空/畸形文件 %q 被判跳过：%+v", p, abs, d)
			}
		}
	}
	w := New(PlatformWindows)
	// 命中项必须带可读理由（写日志/报告用），否则"为什么少了 N 个"无从解释。
	for _, tc := range []struct {
		abs  string
		root bool
		want Kind
	}{
		{`D:\$Recycle.Bin`, false, KindProtectedDir},
		{`D:\pagefile.sys`, true, KindProtectedFile},
		{`D:\CON`, false, KindReservedName},
	} {
		var d Decision
		if tc.want == KindProtectedDir {
			d = w.Dir(tc.abs, base(tc.abs))
		} else {
			d = w.File(base(tc.abs), tc.root)
		}
		if d.Kind != tc.want {
			t.Errorf("%q Kind=%v, want %v", tc.abs, d.Kind, tc.want)
		}
		if !d.Skip || d.Reason == "" {
			t.Errorf("%q 命中却 Skip/Reason 不完整：%+v", tc.abs, d)
		}
		// Reason 的措辞按 Kind 钉住：这三类计数将来要在结果页各占一格，
		// 文案串味（把伪文件说成"保护目录"）会让用户以为清单在吞自己的数据。
		want := map[Kind]string{
			KindProtectedDir:  "保护",
			KindProtectedFile: "伪文件",
			KindReservedName:  "保留设备名",
		}[tc.want]
		if !strings.Contains(d.Reason, want) {
			t.Errorf("%q 的 Reason 应含 %q，得 %q", tc.abs, want, d.Reason)
		}
	}
}
