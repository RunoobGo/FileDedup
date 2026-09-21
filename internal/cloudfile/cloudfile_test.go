package cloudfile

// V1（设计稿 §4.4）：判据的纯参数化断言。
//
// 全部用例**显式传平台**，一个都不看宿主 GOOS（与 internal/sysguard 同一纪律），
// 因此"macOS 的 dataless 位""Windows 的 RECALL 属性位"这些只在别的系统上成立的
// 规则，在 Linux 主门禁里就是可执行断言。
//
// 喂进去的数字是**本机真读数**，不是编的位型（§4.0 E5/E6/E7）：
//
//	0x40008060  iCloud Drive 上 .localized 的 st_flags（SF_DATALESS|UF_HIDDEN|UF_TRACKED|UF_COMPRESSED）
//	0x00008000  同一目录里已下载的 .DS_Store（只有 UF_HIDDEN）
//	0x00000000  本机普通文件（/etc/hosts、go.mod）
//
// 反例必须成对喂：只测"含位判真"的用例挡不住 `flags != 0` 这种偷懒写法，
// 而后者会把整个 iCloud Drive 判成全占位（变异 M-P1-b）。

import (
	"os"
	"testing"
	"time"
)

func TestDarwinJudgesDatalessBit(t *testing.T) {
	for _, flags := range []uint32{
		0x40008060, // 本机真读数
		0x40000000, // 只有 SF_DATALESS
		0x40007fff, // 含位 + 任意低 16 位（UF_SETTABLE 全开）
	} {
		if !From(flags, PlatformDarwin) {
			t.Errorf("From(0x%08x, darwin) = false, want true：SF_DATALESS 已置位却判成普通文件，"+
				"这一类会照常去哈希→触发下载", flags)
		}
	}
}

func TestDarwinDoesNotJudgeEveryFlaggedFileAsPlaceholder(t *testing.T) {
	for _, flags := range []uint32{
		0x00000000, // 普通本地文件
		0x00008000, // 本机真读数：已下载的 iCloud 文件（只有 UF_HIDDEN）
		0x00000060, // UF_TRACKED|UF_COMPRESSED，不含 DATALESS
		0x0000ffff, // UF_SETTABLE 全开
		0x3fffffff, // DATALESS 之下的所有位都开
		0x80000000, // SF_SYNTHETIC 的另一半（0xc0000000 = 含 DATALESS，见下一条）
	} {
		if From(flags, PlatformDarwin) {
			t.Errorf("From(0x%08x, darwin) = true, want false：SF_DATALESS 未置位却判成占位，"+
				"用户已下载的文件会被静默排除出语料", flags)
		}
	}
}

func TestWindowsJudgesRecallBits(t *testing.T) {
	for _, flags := range []uint32{
		0x00400000, // FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS
		0x00040000, // FILE_ATTRIBUTE_RECALL_ON_OPEN
		0x00440000, // 两位同时
		0x00400480, // RECALL_ON_DATA_ACCESS + REPARSE_POINT + NORMAL
	} {
		if !From(flags, PlatformWindows) {
			t.Errorf("From(0x%08x, windows) = false, want true", flags)
		}
	}
}

// Windows 上占位**今天已被 Go 顺带丢掉**（reparse point → ModeIrregular，§4.0 E8），
// 所以本项目的判据不许顺手把普通链接/junction 也算成占位：那些是真的普通不规则文件，
// 归 M21 那一类静默跳过管，不算本项的账。
func TestWindowsReparsePointAloneIsNotPlaceholder(t *testing.T) {
	for _, flags := range []uint32{
		0x00000000,
		0x00000080, // NORMAL
		0x00000400, // REPARSE_POINT 单置（普通符号链接）
		0x00000410, // DIRECTORY|REPARSE_POINT（junction / 目录软链）
		0x00000010, // DIRECTORY
		0x00000420, // REPARSE_POINT|COMPRESSED
		0x00000001, // READONLY
	} {
		if From(flags, PlatformWindows) {
			t.Errorf("From(0x%08x, windows) = true, want false：只有 REPARSE/普通属性而没有 RECALL 位，"+
				"不是云端占位", flags)
		}
	}
}

// Linux「平台未覆盖」必须是断言，而不是"反正读到位是 0"的巧合：
// 有人把 From 改成不看平台（或给 Linux 补一条猜的判据）时，这一条先红。
func TestOtherPlatformNeverJudges(t *testing.T) {
	for _, flags := range []uint32{0x00000000, 0x40008060, 0x00400000, 0xffffffff} {
		if From(flags, PlatformLinux) {
			t.Errorf("From(0x%08x, linux) = true, want false：Linux 无统一占位语义，"+
				"猜判据会把 rclone/gvfs 之外的普通文件吞掉", flags)
		}
	}
}

func TestOfSurvivesMissingPlatformData(t *testing.T) {
	if Of(nil) {
		t.Error("Of(nil) = true, want false：拿不到 FileInfo 时不得判成占位")
	}
	if Of(foreignInfo{}) {
		t.Error("Of(非平台 Stat_t) = true, want false")
	}
}

// foreignInfo 的 Sys() 返回一个非平台类型：在 darwin/windows 上会让字段断言失败。
// 这条用例在 Linux 上是恒真的（flagsOf 本就不看 info），它真正钉的是带 tag 那条腿
// "断言失败→判非占位、且不 panic"。
type foreignInfo struct{}

func (foreignInfo) Name() string       { return "x" }
func (foreignInfo) Size() int64        { return 1 }
func (foreignInfo) Mode() os.FileMode  { return 0 }
func (foreignInfo) ModTime() time.Time { return time.Time{} }
func (foreignInfo) IsDir() bool        { return false }
func (foreignInfo) Sys() any           { return "not a platform Stat_t" }
