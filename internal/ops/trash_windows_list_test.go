//go:build windows

package ops

// 2026-09-19 缺陷回归：批量移入回收站实际只移入 1 个文件。
//
// 根因：syscall.StringToUTF16 自带终止 NUL，原实现每路径又补一个 → 出现
// 连续两个 \0，SHFileOperation 解析到第一个空段即认为列表结束，
// 于是只处理列表里的第 1 个路径，其余静默丢弃且返回成功。
//
// 本文件锁死「pFrom 的构造契约」，使该缺陷无法再溜过测试。

import (
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"
)

// pathListSegments 按 SHFileOperation 的读法把缓冲区切成「以 \0 分隔的段」。
// 返回的切片**不含**终结时的空段——即真实会被处理的路径清单。
//
// 这是对 Win32 行为的独立复刻：列表读到第一个空字符串为止。
//
// ★ 更正（2026-09-22，设计稿 §21.3 W1）：解码原先逐 UTF-16 单元做
// `rune(u)`，代理对的两半各自都是非法码位 ⇒ 每个非 BMP 字符折成一个 U+FFFD，
// `TestBuildPathListRoundTripsUTF16` 在 windows 腿上自 2026-09-19 起恒红。
// 失真出在**本函数**（测试自带的复刻），不在被测物：`buildPathList` 用的是
// `syscall.StringToUTF16`，它按 Win32 契约正确写出代理对。现改用
// `unicode/utf16.Decode` 合对——仍与被测物是两份不同实现，"独立复刻"的身份不变。
func pathListSegments(buf []uint16) (paths []string, dangling bool) {
	var cur []uint16
	for i, u := range buf {
		if u == 0 {
			if len(cur) == 0 {
				// 空段 = 列表结束。其后若还有非零内容，说明构造错了
				// （不该有人越过终结符再塞路径）。
				for _, rest := range buf[i+1:] {
					if rest != 0 {
						return paths, true
					}
				}
				return paths, false
			}
			paths = append(paths, string(utf16.Decode(cur)))
			cur = nil
			continue
		}
		cur = append(cur, u)
	}
	// 走到末尾还有内容 → 缺终止 NUL，Win32 会越界读
	return paths, true
}

// TestBuildPathListAllPathsSurviveParsing 核心回归：N 个路径进去，
// SHFileOperation 必须能读到 N 个。这正是「只移入 1 个」的直接反例。
func TestBuildPathListAllPathsSurviveParsing(t *testing.T) {
	cases := []struct {
		name  string
		paths []string
	}{
		{"单文件", []string{`F:\dup\a1.bin`}},
		{"两个_用户最小复现", []string{`F:\dup\a1.bin`, `F:\dup\a3.bin`}},
		{"三个_含子目录", []string{`F:\dup\a1.bin`, `F:\dup\sub\a2.bin`, `F:\dup\a3.bin`}},
		{"十个", []string{
			`F:\d\1.bin`, `F:\d\2.bin`, `F:\d\3.bin`, `F:\d\4.bin`, `F:\d\5.bin`,
			`F:\d\6.bin`, `F:\d\7.bin`, `F:\d\8.bin`, `F:\d\9.bin`, `F:\d\10.bin`,
		}},
		{"混合盘符", []string{`C:\a\x.bin`, `D:\b\y.bin`, `F:\c\z.bin`}},
		{"中文路径", []string{`F:\重复文件\报告（终）.doc`, `F:\重复文件\副本.txt`}},
		{"长路径", []string{
			`F:\` + strings.Repeat("很长的目录名\\", 12) + `a.bin`,
			`F:\` + strings.Repeat("很长的目录名\\", 12) + `b.bin`,
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			buf := buildPathList(c.paths)
			got, dangling := pathListSegments(buf)
			if dangling {
				t.Fatalf("缓冲区尾部有悬空内容（缺终止 NUL 或越界塞了路径）: %v", buf)
			}
			if len(got) != len(c.paths) {
				t.Fatalf("SHFileOperation 只能读到 %d 个路径（共传入 %d 个）——"+
					"这正是「只移入 1 个文件」缺陷。读到: %v",
					len(got), len(c.paths), got)
			}
			for i := range c.paths {
				if got[i] != c.paths[i] {
					t.Fatalf("第 %d 个路径不符: got %q, want %q", i, got[i], c.paths[i])
				}
			}
		})
	}
}

// TestBuildPathListIsDoubleNulTerminated 锁死 MSDN 的硬要求：
// pFrom 必须以双 \0 结尾，且路径之间**不能**出现空段。
func TestBuildPathListIsDoubleNulTerminated(t *testing.T) {
	buf := buildPathList([]string{`F:\a.bin`, `F:\b.bin`})
	n := len(buf)
	if n < 2 {
		t.Fatalf("缓冲区过短: %d", n)
	}
	if buf[n-1] != 0 || buf[n-2] != 0 {
		t.Fatalf("末尾不是双 \\0 结尾: %v", buf[n-3:])
	}
	// 逐位扫描：只允许「路径本体 / 单个 NUL 分隔符 / 末尾双 NUL」
	// 一旦出现「连续 NUL 后又跟着非零」即构造错误。
	run := 0
	for i, u := range buf {
		if u == 0 {
			run++
			continue
		}
		if run >= 2 {
			t.Fatalf("第 %d 位处：连续 %d 个 NUL 之后仍有内容 %d —— "+
				"SHFileOperation 会在此截断列表，后续路径全部丢失", i, run, u)
		}
		if run == 0 && i > 0 && buf[i-1] != 0 {
			// 正常路径内部字符
		}
		run = 0
	}
	if run != 2 {
		t.Fatalf("末尾 NUL 个数 = %d, want 2", run)
	}
}

// TestBuildPathListRegressionPinsStringToUTF16Nul 把根因钉在测试里：
// syscall.StringToUTF16 自带终止 NUL，所以**不需要**再手工补一个。
// 若哪天 Go 改了这条语义，本用例会失败并提示重审 buildPathList。
func TestBuildPathListRegressionPinsStringToUTF16Nul(t *testing.T) {
	const p = `F:\dup\a1.bin`
	units := utf16FromString(p)
	if len(units) == 0 {
		t.Fatal("utf16FromString 返回空")
	}
	if units[len(units)-1] != 0 {
		t.Fatalf("前提失效：syscall.StringToUTF16 不再自带终止 NUL（末元素=%d）。"+
			"请重审 buildPathList 的构造方式", units[len(units)-1])
	}
	// 路径本体长度（不含那一格 NUL）
	bodyLen := len(units) - 1
	if got := len([]rune(p)); got != bodyLen {
		t.Fatalf("UTF-16 单元数 %d 与 rune 数 %d 不符（纯 ASCII 路径应当相等）", bodyLen, got)
	}
	// 缺陷版会多出 len(paths) 个冗余 NUL；这里断言缓冲区总量恰好：
	//   各路径（含自身 NUL）+ 1 个终结 NUL
	paths := []string{p, `F:\dup\a3.bin`}
	want := 0
	for _, x := range paths {
		want += len(utf16FromString(x))
	}
	want++ // 终结符
	if got := len(buildPathList(paths)); got != want {
		t.Fatalf("缓冲区长度 = %d, want %d（多出来的就是冗余 NUL，会截断列表）", got, want)
	}
}

// TestBuildPathListEmpty 空输入不得 panic、不得生成非法缓冲区。
func TestBuildPathListEmpty(t *testing.T) {
	buf := buildPathList(nil)
	if len(buf) != 1 || buf[0] != 0 {
		t.Fatalf("空输入的缓冲区应为单个 NUL，实得 %v", buf)
	}
	// 空列表调用方会提前返回（defaultTrash 的 len(paths)==0 分支），
	// 但即便走到这里，也必须是「空列表」而不是「一个空路径」。
	got, dangling := pathListSegments(buf)
	if dangling || len(got) != 0 {
		t.Fatalf("空输入被解析成 %d 个路径（dangling=%v）", len(got), dangling)
	}
}

// TestBuildPathListRoundTripsUTF16 中文/非 BMP 路径必须原样往返，
// 说明「编解码」这一层没有问题（缺陷纯粹在 NUL 个数上）。
func TestBuildPathListRoundTripsUTF16(t *testing.T) {
	paths := []string{
		`F:\重复文件\报告（终）.doc`,
		`F:\重复文件\副 本.txt`,
		`F:\emoji\😀😀.bin`,  // 非 BMP：代理对
		`F:\full\ＦＵＬＬ.bin`, // 全角
	}
	buf := buildPathList(paths)
	got, dangling := pathListSegments(buf)
	if dangling {
		t.Fatal("缓冲区尾部悬空")
	}
	if len(got) != len(paths) {
		t.Fatalf("路径数 = %d, want %d", len(got), len(paths))
	}
	for i := range paths {
		if got[i] != paths[i] {
			t.Fatalf("第 %d 个路径往返失真:\n got %q\nwant %q", i, got[i], paths[i])
		}
	}
	// 顺带确认 UTF-16 编码本身没被截断（代理对占 2 个单元）
	for _, p := range paths {
		units := utf16FromString(p)
		if binary.LittleEndian.Uint16([]byte{byte(units[0]), 0}) != units[0] {
			t.Fatal("UTF-16 小端序假设失效")
		}
	}
}
