package main

// startup_failure.go —— 启动失败的那条**不依赖 stderr** 的出口（M283）。
//
// 现场：发布版 Windows 是 GUI 子系统（/SUBSYSTEM:WINDOWS），没有附加控制台。
// main() 里 `fmt.Fprintf(os.Stderr, "FileDedup 启动失败: %v")` 落进一个不存在的管道，
// 于是 wails.Run 失败（WebView2 Runtime 缺失、资产装配失败、Linux 无 X/Wayland 会话）
// 在用户侧只呈现为"双击没反应"——零可诊断痕迹。
//
// 三条候选通道里选"同目录滚动小日志"的理由（登记表 04 §6.51 M283 给了三条）：
//   - 起窗后红条：前提是窗口起得来，而本缺陷的现场恰恰是**窗口根本没出现**，红条无处可挂；
//   - Windows 事件日志：平台专属，darwin/linux 各要另一套，取证还要事件查看器；
//   - 同目录小日志：与 cache.db / history.db 同目录（用户手册已教过用户去哪找），
//     三平台同一份代码，且**不依赖任何还在启动中的子系统**。
//
// 上限 32 KiB 是"小"字的兑现：启动失败会被反复双击，每次一条；配置目录又常被同步盘
// 接管，无界增长会成为第二起故障。超出即弃旧留新，裁切只落在行边界上。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// startupFailureLogName 日志文件名。刻意不叫 filededup.log——这个名字要出现在
	// stderr 那句里，用户拿着它去手册里搜，能搜到比"通用日志"更明确的指向。
	startupFailureLogName = "startup-error.log"
	// startupFailureLogMax 整文件上限（字节）。
	startupFailureLogMax = 32 << 10
)

// startupFailureDir 决定日志落点：已解析的配置目录 > os.UserConfigDir()/FileDedup > 临时目录。
//
// 纯函数、不碰文件系统——判据要能在任何机器上断言（含 Linux CI）。
// 第三档不是装饰：os.UserConfigDir() 在本仓既有判据里就是会失败的那个调用
// （见 App.startup 里 cfgDir 的兜底与 M60），而"取不到配置目录"与"窗口起不来"
// 高度相关——恰好是最需要留下痕迹的那类机器。
func startupFailureDir(cfgDir, userConfigDir string) string {
	if cfgDir != "" {
		return cfgDir
	}
	if userConfigDir != "" {
		return filepath.Join(userConfigDir, "FileDedup")
	}
	return filepath.Join(os.TempDir(), "FileDedup")
}

// writeStartupFailureLog 追加一条启动失败记录，返回日志路径；返回空串表示没写成。
//
// 空串是**允许**的结果而不是错误：这条出口存在的意义是"别什么都不留"，
// 若连目录都建不出来（只读盘、坏盘），也不能因为写日志失败而把进程卡死或 panic——
// 那正是它要诊断的那类机器。调用方（main）此时退回只有 stderr 的形状。
func writeStartupFailureLog(dir string, err error) string {
	// dir 为空时宁可不写：日志会落到进程当前工作目录（发布版的 CWD 可能是
	// Program Files 或用户家目录），把"留痕迹"做成"污染别人家"。
	if dir == "" {
		return ""
	}
	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
		return ""
	}
	p := filepath.Join(dir, startupFailureLogName)

	line := []byte(fmt.Sprintf("%s 启动失败: %v\n", time.Now().Format(time.RFC3339), err))
	if len(line) > startupFailureLogMax { // 单条就超上限（错误串极长）⇒ 截尾保头
		line = append(line[:startupFailureLogMax-4], "...\n"...)
	}
	// 旧内容只在"加上这一条会越界"时才读回并裁尾；本文件由本函数维持有界，
	// 所以这次读入在实践中不超过上限（异常外部写大的那份照样裁到窗口内）。
	old, _ := os.ReadFile(p) // 读不到＝没有旧日志（首次失败/无权限），不影响写入
	if keep := startupFailureLogMax - len(line); len(old) > keep {
		if keep <= 0 {
			old = nil
		} else {
			old = old[len(old)-keep:]
			// 裁切点多半落在某一行中间——丢掉那半行，否则日志开头是一条残缺记录，
			// 下一位读者会怀疑整份文件。
			if i := strings.IndexByte(string(old), '\n'); i >= 0 {
				old = old[i+1:]
			} else {
				old = nil
			}
		}
	}
	out := make([]byte, 0, len(old)+len(line))
	out = append(out, old...)
	out = append(out, line...)
	if wErr := os.WriteFile(p, out, 0o644); wErr != nil {
		return ""
	}
	return p
}
