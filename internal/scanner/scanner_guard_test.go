package scanner

// M6-P4（2026-09-21）系统保护清单的遍历层集成探针（设计稿 §2.8 的 T6~T11）。
//
// 分层：判据本体的行为由 internal/sysguard 的纯函数用例覆盖；这里只测
// "清单在真实遍历里产生了什么后果"——剪枝、计数、与隐藏规则的先后、逃逸通道。
//
// 平台无关的做法：$Recycle.Bin 与 lost+found 在清单里是**三平台通吃**的条目
// （外接卷跨平台挂载才是主要受害场景），所以 T6/T8/T9 不需要任何平台豁免，
// 在 Linux 门禁上就是真夹具真断言。只有盘根伪文件与 Windows 保留名必须
// 换装 Windows 清单——走 guard 注入点（与 probeCaseVerdict 同一手法）。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/model"
	"filededup/internal/sysguard"
)

func setGuard(t *testing.T, g *sysguard.Guard) {
	t.Helper()
	prev := guard
	guard = g
	t.Cleanup(func() { guard = prev })
}

// hasPathWith 结果集里是否有路径含子串 frag（用绝对路径拼接避免分隔符假设）。
func hasPathWith(res *Result, frag string) bool {
	for _, e := range res.Files {
		if strings.Contains(filepath.ToSlash(e.Path), frag) {
			return true
		}
	}
	return false
}

func TestWalkPrunesProtectedDirAndCountsIt(t *testing.T) {
	root := t.TempDir()
	mkDirFiles(t, filepath.Join(root, "$Recycle.Bin", "S-1-5-21-1"), "dup.bin")
	mkDirFiles(t, filepath.Join(root, "keep"), "a.bin")

	res := Walk(context.Background(), []string{root}, &model.Filters{}, 2)
	// 修前必红的一刻：修正前 $Recycle.Bin 不以 "." 开头，整棵子树照常入语料，
	// 于是"移入回收站 → 再扫盘根"会把已删副本配成重复组，用户照着再删一次。
	if hasPathWith(res, "/$Recycle.Bin/") {
		t.Error("回收站实体目录进了语料")
	}
	if !hasPathWith(res, "/keep/a.bin") {
		t.Error("普通目录被误伤（清单过宽）")
	}
	if res.ProtectedDirs < 1 {
		t.Errorf("ProtectedDirs = %d, want >=1（剪枝必须可见地计数，静默跳过等于计数说谎）", res.ProtectedDirs)
	}
	// 保护跳过**不是失败**：记进 Failed 会把失败抽屉灌满，真正的读不了被淹没。
	if len(res.Failed) != 0 {
		t.Errorf("Failed = %+v, want 空", res.Failed)
	}
}

func TestWalkCountsRootPseudoFileOnly(t *testing.T) {
	setGuard(t, sysguard.New(sysguard.PlatformWindows))
	root := t.TempDir()
	mkDirFiles(t, root, "pagefile.sys")                       // 盘根伪文件
	mkDirFiles(t, filepath.Join(root, "sub"), "pagefile.sys") // 镜像备份里的同名文件=用户数据

	res := Walk(context.Background(), []string{root}, &model.Filters{}, 2)
	if res.ProtectedFiles != 1 {
		t.Errorf("ProtectedFiles = %d, want 1", res.ProtectedFiles)
	}
	if hasPathWith(res, "/pagefile.sys") && !hasPathWith(res, "/sub/pagefile.sys") {
		t.Error("只挡了根级却把嵌套的也挡了（或反之）")
	}
	n := 0
	for _, e := range res.Files {
		if filepath.Base(e.Path) == "pagefile.sys" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("pagefile.sys 采集数 = %d, want 1（只应剩嵌套那份）", n)
	}
}

func TestWalkReservedNameWindowsGuardOnly(t *testing.T) {
	root := t.TempDir()
	// 夹具本身在 Windows 上**造不出来**：CreateFile("CON.txt") 返回
	// ERROR_INVALID_NAME——保留名根本不允许作为文件名存在，而这恰恰是判据要
	// 挡住的形状。所以这里探测建不成就 Skip，而不是把它加进
	// scripts/test-windows-quarantine.sh：按该脚本 2026-09-20 的约定，环境不支持
	// 的用例应在测试内自探，隔离清单只用于"代码在该平台确实有问题"。
	// 判据本体的覆盖面不受影响——internal/sysguard 的纯函数用例在任一平台都
	// 全量执行 Windows 规则（平台以参数注入，见设计稿 §2.2 理由 4）。
	probe, perr := os.Create(filepath.Join(root, "CON.txt"))
	if perr != nil {
		t.Skipf("本平台无法创建保留名夹具（%v）；保留名判据由 internal/sysguard 用例覆盖", perr)
	}
	if _, werr := probe.Write([]byte("x")); werr != nil {
		probe.Close()
		t.Skipf("本平台无法向保留名夹具写入（%v）", werr)
	}
	probe.Close()
	mkDirFiles(t, root, "notes.txt")

	setGuard(t, sysguard.New(sysguard.PlatformWindows))
	res := Walk(context.Background(), []string{root}, &model.Filters{}, 2)
	if hasPathWith(res, "/CON.txt") {
		t.Error("Windows 保留名文件进了语料——对它做操作打到的是设备")
	}
	if res.ProtectedFiles != 1 {
		t.Errorf("ProtectedFiles = %d, want 1", res.ProtectedFiles)
	}

	// 平台不外溢的反向断言：Linux 清单下 CON.txt 是完全合法的用户文件。
	setGuard(t, sysguard.New(sysguard.PlatformLinux))
	res = Walk(context.Background(), []string{root}, &model.Filters{}, 2)
	if !hasPathWith(res, "/CON.txt") {
		t.Error("Linux 清单误套了 Windows 保留名规则（用户文件被静默漏扫）")
	}
	if res.ProtectedFiles != 0 {
		t.Errorf("ProtectedFiles = %d, want 0", res.ProtectedFiles)
	}
}

func TestRootInsideProtectedDirStillScanned(t *testing.T) {
	root := t.TempDir()
	inner := filepath.Join(root, "lost+found", "inner")
	mkDirFiles(t, inner, "x.bin")
	mkDirFiles(t, filepath.Join(root, "keep"), "a.bin")

	// 两根：宽根 + 落在受保护目录**内部**的根。dedupeRoots 会因为后者被前者覆盖
	// 而丢弃它，于是逃逸判据必须回到"用户原始指定的根"上去看，否则
	// lost+found 被剪枝、用户点名的 inner 一个文件也扫不到，结果页显示 0 组
	// 且没有任何解释。
	res := Walk(context.Background(), []string{root, inner}, &model.Filters{}, 2)
	if !hasPathWith(res, "/lost+found/inner/x.bin") {
		t.Error("扫描根位于保护目录内部时，该根的内容被剪枝掉了")
	}
	if len(res.UnprotectedRoots) != 1 || filepath.ToSlash(res.UnprotectedRoots[0]) != filepath.ToSlash(inner) {
		t.Errorf("UnprotectedRoots = %v, want [%s]（逃逸必须指名是哪个根脱离了保护）", res.UnprotectedRoots, inner)
	}
	if len(res.Failed) != 0 {
		t.Errorf("Failed = %+v, want 空", res.Failed)
	}
}

func TestExplicitProtectedRootIsReported(t *testing.T) {
	root := t.TempDir()
	rec := filepath.Join(root, "$Recycle.Bin")
	mkDirFiles(t, rec, "gone.bin")

	// 用户点名要扫的根本身就是清单内路径：照他的意思扫，但必须留下记录，
	// 界面据此弹"已脱离系统保护"警示（M8）。
	setGuard(t, sysguard.New(sysguard.PlatformWindows))
	res := Walk(context.Background(), []string{rec}, &model.Filters{}, 2)
	if !hasPathWith(res, "/$Recycle.Bin/gone.bin") {
		t.Error("显式设为扫描根的受保护目录被自己的剪枝挡住了")
	}
	if len(res.UnprotectedRoots) != 1 {
		t.Errorf("UnprotectedRoots = %v, want 1 项", res.UnprotectedRoots)
	}
	if res.ProtectedDirs != 0 {
		t.Errorf("ProtectedDirs = %d, want 0（根本身不参与剪枝计数）", res.ProtectedDirs)
	}
}

func TestHiddenFlagDoesNotMoveProtectedCount(t *testing.T) {
	root := t.TempDir()
	mkDirFiles(t, filepath.Join(root, ".Spotlight-V100"), "idx.bin")
	mkDirFiles(t, filepath.Join(root, "keep"), "a.bin")

	// 既属"系统保护"又属"隐藏"的条目，必须先判保护：否则用户勾一下
	// "包含隐藏文件"，"已保护跳过 N"就凭空变小，读起来像保护失效。
	off := Walk(context.Background(), []string{root}, &model.Filters{IncludeHidden: false}, 2)
	on := Walk(context.Background(), []string{root}, &model.Filters{IncludeHidden: true}, 2)
	if off.ProtectedDirs != on.ProtectedDirs {
		t.Errorf("ProtectedDirs 随 IncludeHidden 变化：%d → %d", off.ProtectedDirs, on.ProtectedDirs)
	}
	if off.ProtectedDirs < 1 {
		t.Errorf("ProtectedDirs = %d, want >=1", off.ProtectedDirs)
	}
	for _, r := range []*Result{off, on} {
		if hasPathWith(r, "/.Spotlight-V100/") {
			t.Error("卷元数据目录进了语料")
		}
	}
}

func TestProtectedDirsCountIsDirCountNotFileGuess(t *testing.T) {
	// 剪枝没有下潜，因此只能报目录数。报成"跳过了 N 个文件"就是编数
	// ——这一条钉住 §1 约束 2（计数不许说谎）。
	root := t.TempDir()
	deep := filepath.Join(root, "lost+found", "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	mkDirFiles(t, deep, "1.bin", "2.bin", "3.bin")
	mkDirFiles(t, filepath.Join(root, "keep"), "a.bin")

	res := Walk(context.Background(), []string{root}, &model.Filters{}, 2)
	if res.ProtectedDirs != 1 {
		t.Errorf("ProtectedDirs = %d, want 1（一个被剪枝的目录，不管它里面有多少文件）", res.ProtectedDirs)
	}
}
