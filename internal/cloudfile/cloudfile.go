// Package cloudfile 判定一个文件是不是「云端占位文件」（M6-P1，2026-09-21）。
//
// 为什么需要它（04 §6.7 C 组 1 / 总纲 §2.1）：OneDrive / iCloud Drive / Google Drive
// 这类同步盘会在本地放一个**只有元数据、内容还在云端**的占位文件。它的逻辑大小是真的
// （所以会进同一个尺寸桶），读它却会把整份下载回来。两平台的实情不一样：
//
//   - macOS：占位对象的 mode 就是普通文件（本机实测 `-rw-------`，§4.0 E5），
//     今天会一路走到 pipeline 的预筛采样读取（hasher 首尾各 64 KiB）→ 真触发下载；
//   - Windows：占位是 reparse point，Go 的 mode() 会顺手给 ModeIrregular，于是
//     被 scanner 的 !IsRegular() 丢掉——**内容不会下载，但一个计数都没有**。
//     用户扫 OneDrive 看到"0 个重复组"，无从分辨是"没重复"还是"300 个占位被跳过"。
//
// 所以本包的第一价值是**让这一类可见**，其次才是省流量。
//
// 分层约定（与 internal/sysguard、internal/realbytes 同族）：
//   - 本文件无 build tag，承载**全部判定规则**（From）与平台常量的真值——因此在
//     Linux 主门禁里，macOS 的 dataless 位与 Windows 的 RECALL 位都是可执行断言；
//   - 带 tag 的文件只做两件事：给出 Current、把标志位从 os.FileInfo 里读出来
//     （两平台都是**零额外 syscall**：值就在遍历期 de.Info() 已经拿到的那份 stat 里）。
package cloudfile

import "os"

// Platform 决定用哪一套位语义。同一个 uint32 在两个平台上字段含义完全不同
// （darwin 是 st_flags，windows 是 dwFileAttributes），所以判定必须连平台一起给。
// 与 sysguard 同一命名口径：非 windows/darwin 一律按 PlatformLinux 处理。
type Platform int

const (
	PlatformLinux Platform = iota
	PlatformDarwin
	PlatformWindows
)

// macOS st_flags 位。真值出处（两条独立来源相符）：
//
//	$SDK/usr/include/sys/stat.h:359  #define SF_DATALESS 0x40000000  /* file is dataless object */
//	$GOMODCACHE/golang.org/x/sys@v0.48.0/unix/zerrors_darwin_{amd64,arm64}.go:1286
//
// Go 自带的 syscall（darwin）未导出任何 SF_ 常量，故本地定义并附出处——
// 与 internal/ops/crossdevice_windows.go:10-23 抄录 Win32 错误码同一手法。
const sfDataless = 0x40000000

// Windows 文件属性位。真值出处：
// $GOMODCACHE/golang.org/x/sys@v0.48.0/windows/types_windows.go:114,122,123
//
//	FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS = 0x00400000  // 数据在云端，按需召回
//	FILE_ATTRIBUTE_RECALL_ON_OPEN        = 0x00040000  // 打开时召回
//
// 为什么用属性位而不是总纲 §2.1 原写的 reparse tag ∈ 0x9000xxxx：扫描阶段刻意
// 不开句柄（见 internal/scanner/filekey_windows.go 的取舍），而 tag 既没有现成
// 常量（`IO_REPARSE_TAG_CLOUD` 在 x/sys 的 windows 包里检索零命中）、也不随
// os.FileInfo.Sys() 回吐（$GOROOT/src/os/types_windows.go:275-286 只给
// Win32FileAttributeData，不含 fileStat.ReparseTag）。属性位免费可得且语义正是
// "数据不在本地"，够用。
const (
	fileAttrRecallOnDataAccess = 0x00400000
	fileAttrRecallOnOpen       = 0x00040000
)

// From 是全部判据所在。三条规则，每条都有对应用例与对应变异（设计稿 §4.4）：
//
//  1. 平台不认识 → false。Linux 尤其要 false：rclone/gvfs/lookback 的占位没有统一
//     语义，**猜一条判据的代价是把普通文件吞出语料**，比偶尔多下载一份更糟；
//  2. darwin 只看 SF_DATALESS 这一位，**不许写成 flags != 0**——本机实测一个
//     已下载的 iCloud 文件就带着 UF_HIDDEN（0x00008000），那样会把整片云盘判成占位；
//  3. windows 只看两个 RECALL 位。单独的 REPARSE_POINT 是符号链接/junction 共有的位，
//     不属于本项的账（那些文件走 !IsRegular() 那条既有的静默跳过）。
//
// 读不到标志位时调用方传 0，两条平台规则都自然给出 false——即"判不了就照常扫描"，
// 与 internal/realbytes 的 known=false 同方向但相反处置：那里保守=退回逻辑数字，
// 这里保守=**不跳过**，因为错误跳过会同时虚报计数并吞掉一个真重复候选。
func From(flags uint32, p Platform) bool {
	switch p {
	case PlatformDarwin:
		return flags&sfDataless != 0
	case PlatformWindows:
		return flags&(fileAttrRecallOnDataAccess|fileAttrRecallOnOpen) != 0
	default:
		return false
	}
}

// Of 取单个文件的判定。info 传遍历期 de.Info() 已经拿到的那份，零额外 syscall。
func Of(info os.FileInfo) bool {
	flags, _ := flagsOf(info)
	return From(flags, Current)
}
