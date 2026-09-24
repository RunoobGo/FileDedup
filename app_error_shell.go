// app_error_shell.go：错误事件的中文外壳（M202，2026-09-24 裁定「统一中文外壳 +
// 原文降级为 detail」；设计稿 `2026-09-24-error-shell-m202-design`）。
//
// 改前三个发射点把 `err.Error()` 原样 emit 给前端 ⇒ 界面直接出现英文 OS 错误
// （`open /Users/…: permission denied`）与内部绝对路径。
//
// 外壳是**四档**，顺序即优先级（这一格的全部难度在"不能一律套外壳"）：
//
//	D panic 腿（含 `goroutine panic`/`panic: `）→ 中文壳，原句整体降级进 detail。
//	  ★ 必须排在 B 前面：panic 文本的语言由抛它的运行时/第三方决定，见 panicShell 注。
//	B 应用自撰中文（串中含 CJK）→ 原文一字不改。本仓大量错误本来就是给用户看的
//	  中文句子（回收站拒绝句、身份拦截句，后者还把底层 OS 原因**原样嵌在句子里**），
//	  给它们再包一层壳等于把已有信息降级——所以 CJK 判据必须走在签名表**前面**。
//	A 已知系统错误签名（纯英文串命中 errno 尾语）→ 换成中文句，路径不进外壳。
//	C 认不出 → 显式"未分类"，★ 不假装知道是什么。
//
// 原文（含绝对路径）一律留在 detail：排查与"复制这条错误"必须有，截图外发留意
// （用户可见口径写在 docs/09 §6.5「错误提示的两段式」与 docs/10 §3.9）。
package main

import (
	"fmt"
	"strings"
)

// errorShellRule 的 needle 一律小写，匹配前把错误串 ToLower。
type errorShellRule struct {
	needle string
	shell  string
}

// panicShell：panic 腿专用，判据**排在 B 类（含 CJK 即原文照出）之前**。
// 理由：panic 的文本语言由抛它的运行时或第三方决定（测试里就见过
// `scan goroutine panic: 模拟流水线内部 panic` 这种中英混排），不能因为"含中文"
// 就当成"应用自撰的用户可读文案"原样送出去。
const panicShell = "应用内部异常已被拦截，本次操作中断"

func isPanicText(lower string) bool {
	return strings.Contains(lower, "goroutine panic") || strings.Contains(lower, "panic: ")
}

// errorShellTable：只认**签名片段**，不认整句。顺序即优先级，首个命中即返回。
// ★ unix 与 Windows 的 errno 字面不同（Go 在 Windows 折成 `The system cannot find the file
// specified.` 一类），两族都必须在册，否则同一条 OS 错误在两条腿上一条有壳、一条没壳。
var errorShellTable = []errorShellRule{
	{"context canceled", "操作已取消"},
	{"context deadline exceeded", "操作超时"},
	{"operation canceled", "操作已取消"},
	{"permission denied", "权限不足，无法访问该位置"},
	{"access is denied", "权限不足，无法访问该位置"},
	{"no such file or directory", "该文件或文件夹已不存在"},
	{"the system cannot find the file", "该文件或文件夹已不存在"},
	{"the system cannot find the path", "该文件或文件夹已不存在"},
	{"is a directory", "路径类型不符：该项是目录"},
	{"not a directory", "路径类型不符：该项不是目录"},
	{"device or resource busy", "正被其他程序占用"},
	{"being used by another process", "正被其他程序占用"},
	{"sharing violation", "正被其他程序占用"},
	{"resource deadlock", "正被其他程序占用（资源锁冲突）"},
	{"read-only file system", "所在卷是只读的"},
	{"no space left on device", "目标卷空间已满"},
	{"file name too long", "路径过长，超过文件系统上限"},
	{"too many open files", "打开的文件过多，请稍后重试"},
	{"cross-device link", "跨卷链接不被支持（硬链接仅同卷可用）"},
	{"operation not supported", "该卷或该操作不被支持"},
	{"not supported", "该卷或该操作不被支持"},
}

// hasCJKText 判"是否应用自撰中文句"：命中 CJK 统一表意文字基本区 + 扩展 A + 标点。
func hasCJKText(s string) bool {
	for _, r := range s {
		switch {
		case r >= 0x3001 && r <= 0x303F, // 、。〈〉《》「」
			r >= 0x3400 && r <= 0x4DBF, // 扩展 A
			r >= 0x4E00 && r <= 0x9FFF, // 基本区
			r >= 0xF900 && r <= 0xFAFF: // 兼容表意
			return true
		}
	}
	return false
}

// errorShell 返回（中文外壳, 原文 detail）。detail 与外壳相同时由 errorEvent 省掉，
// 不让前端多一个空串分支。
func errorShell(err error) (string, string) {
	if err == nil {
		return "", ""
	}
	return shellOfErrorText(err.Error()), err.Error()
}

func shellOfErrorText(raw string) string {
	lower := strings.ToLower(raw)
	if isPanicText(lower) {
		return panicShell
	}
	if hasCJKText(raw) {
		return raw
	}
	for _, rule := range errorShellTable {
		if strings.Contains(lower, rule.needle) {
			return rule.shell
		}
	}
	return "操作未能完成（未分类的系统错误）"
}

// errorEvent 产出事件载荷：{"error": 外壳} + 可选 {"detail": 原文}。
// ★ 载荷仍是 map[string]string，前端 `errText` 读 `error` 的既有路径不变（加键不破坏契约）。
func errorEvent(err error) map[string]string {
	shell, detail := errorShell(err)
	out := map[string]string{"error": shell}
	if detail != "" && detail != shell {
		out["detail"] = detail
	}
	return out
}

// shellRPCError 给 RPC **返回腿**套同一套中文外壳（M214）。
//
// 改前 event 通道已被 errorEvent 收编，但方法直接 return 的 err 仍是英文 OS 原句
// （`open /Users/…: permission denied`）——前端 toast 的 `errText` 把它原样转字符串上屏。
// 本函数把裸 err 转成「中文外壳（系统原文：裸串）」，与 09 §"错误文案"已立的"系统原文"话术同形。
//
// 判据（不能一律套）：errorShell 得 (shell, raw)；
//   - shell==raw（B 档应用自撰中文，含 ErrCorruptDisabled / ErrEvictFailed 这些哨兵）
//     ⇒ **原样返回 err**（不是新错误值），保留 errors.Is/errors.As 的身份与包装链，
//     ★ 绝不给已可读的中文句再包一层壳（信息降级 + 破坏哨兵比对）；
//   - shell!=raw（A 档英文 errno 命中签名 / C 档未分类 / D 档 panic）⇒ 合成"外壳（系统原文：裸串）"。
func shellRPCError(err error) error {
	if err == nil {
		return nil
	}
	shell, raw := errorShell(err)
	if shell == raw {
		return err
	}
	return fmt.Errorf("%s（系统原文：%s）", shell, raw)
}
