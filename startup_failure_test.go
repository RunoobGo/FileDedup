package main

// M283（2026-09-27 实施批；04 §6.51 立账，源头是 W9-9 的真机读数）：
// 发布版是 GUI 子系统（Windows 的 /SUBSYSTEM:WINDOWS），**没有附加控制台**——
// main() 里那句 fmt.Fprintf(os.Stderr, "FileDedup 启动失败: %v") 落进一个不存在的管道，
// 用户看到的只是"双击没反应"，第三次双击、第四天重装、最后把 issue 提到我这里，
// 全程零可诊断痕迹。stderr 那一行不是错的，它是**不够**：它依赖一个 GUI 进程没有的东西。
//
// 三条候选通道里为什么选同目录滚动小日志（登记表的取向给了三条）：
//   - 「起窗后红条」：前提是窗口起得来。本缺陷的现场恰恰是 wails.Run 直接返回错误
//     （WebView2 Runtime 缺失、资产装配失败、Linux 无 X/Wayland 会话）——窗口根本没出现，
//     红条无处可挂。
//   - 「Windows 事件日志」：平台专属，darwin/linux 各要另一套；而且取证要管理员看事件查看器。
//   - 「同目录滚动小日志」：与 cache.db / history.db 同目录（用户手册已经教过用户去哪找），
//     三平台同一份代码，写完即可用，且**不依赖任何还在启动中的子系统**。
//
// 判据形状沿用仓库惯例：判断放在纯函数里（startupFailureDir 不碰文件系统），
// 只有落盘这一步取真实环境；main() 的接线用源码锚点钉——运行时那条路在测试里走不到
// （走到就是起一个真窗口），这与 M196 把装配从 main() 抽出 buildAppOptions 是同一道题。

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 修前必红：这条出口必须存在（M283 的"可诊断痕迹"就是这几个字面量）。
func TestWriteStartupFailureLogLeavesTraceOnDisk(t *testing.T) {
	dir := t.TempDir()
	p := writeStartupFailureLog(dir, errors.New("WebView2 Runtime not found: loader 0x80070002"))
	want := filepath.Join(dir, startupFailureLogName)
	if p != want {
		t.Fatalf("日志路径 = %q，期望 %q", p, want)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("日志文件没落盘: %v", err)
	}
	line := string(raw)
	for _, want := range []string{"启动失败", "WebView2 Runtime not found", "0x80070002"} {
		if !strings.Contains(line, want) {
			t.Fatalf("日志缺少「%s」：%q", want, line)
		}
	}
	// 没有时间的日志无法回答"是哪一次双击"，而多次双击正是这类故障的常态。
	if !regexp.MustCompile(`20\d\d-\d\d-\d\dT\d\d:\d\d:\d\d`).MatchString(line) {
		t.Fatalf("日志应有本地时间戳（多次重试要能分清是哪一次）：%q", line)
	}

	// 追加而非覆写：第二次失败不许抹掉第一次的读数
	p2 := writeStartupFailureLog(dir, errors.New("第二次：窗口样式资源损坏"))
	if p2 != want {
		t.Fatalf("同目录两次写应指向同一文件: %q", p2)
	}
	raw2, err := os.ReadFile(p2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw2), "WebView2") || !strings.Contains(string(raw2), "第二次") {
		t.Fatalf("两次失败都应在场（追加语义）：%q", raw2)
	}
	if n := strings.Count(string(raw2), "启动失败"); n != 2 {
		t.Fatalf("期望 2 条记录，实得 %d：%q", n, raw2)
	}
}

// 滚动：这是**小**日志，不是无界增长。启动失败会反复双击，每次一条；
// 但同一路径也可能被别的写者占着（用户把配置目录放在同步盘里），无界增长就是第二起故障。
func TestWriteStartupFailureLogStaysBounded(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, startupFailureLogName)
	// 预置一份远超上限的旧日志（每行都以时间戳开头，便于验证裁切落在行边界）
	var sb strings.Builder
	for i := 0; i < 4000; i++ {
		sb.WriteString("2026-01-01T00:00:00+08:00 启动失败: filler line\n")
	}
	if err := os.WriteFile(p, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := writeStartupFailureLog(dir, errors.New("本轮的真错误")); got == "" {
		t.Fatal("写入失败")
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > startupFailureLogMax {
		t.Fatalf("日志未收敛：%d 字节 > 上限 %d", len(raw), startupFailureLogMax)
	}
	if !strings.Contains(string(raw), "本轮的真错误") {
		t.Fatalf("裁旧不许裁新（最新一条必须在场）：%q", string(raw)[:120])
	}
	// 只保留尾部 ⇒ 必须落在行边界上，半行会让下一位读者以为日志坏了
	for i, ln := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if !regexp.MustCompile(`^20\d\d-\d\d-\d\dT`).MatchString(ln) {
			t.Fatalf("第 %d 行不是完整记录（裁切落在行中）：%q", i, ln)
		}
	}
}

// dir 为空时**不写**：日志路径为空串，调用方（main）退回只有 stderr 的形状。
// 反过来说，绝不能往进程当前工作目录里丢一个 startup-error.log —— 发布版的 CWD
// 可能正是 Program Files 或用户家目录，那是把"诊断"变成"污染"。
func TestWriteStartupFailureLogRefusesEmptyDir(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got := writeStartupFailureLog("", errors.New("任何错误")); got != "" {
		t.Fatalf("空目录应返回空串，实得 %q", got)
	}
	if _, err := os.Stat(filepath.Join(cwd, startupFailureLogName)); err == nil {
		t.Fatal("空目录时把日志写进了进程工作目录")
	}
	// 建不出目录的 loc 同样返回空串而不是 panic（GUI 起不来时磁盘可能正是坏的）。
	// 造法必须是"路径上有一环是普通文件"——直接在临时目录下建个子目录是会成功的，
	// 而往工作目录建探针子目录等于每跑一次测试就往仓库里留一件垃圾。
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := writeStartupFailureLog(filepath.Join(blocker, "sub"), errors.New("x")); got != "" {
		t.Fatalf("目录建不出来时应返回空串，实得 %q", got)
	}
}

// 落点选择的判据（纯函数，不碰真实用户目录）：
// 已解析的配置目录 > os.UserConfigDir()/FileDedup > 临时目录。
// 第三档不是可有可无：os.UserConfigDir() 在本仓的既有判据里就是会失败的
// （app.go 的 cfgDir 兜底、M60），而"取不到配置目录"与"窗口起不来"高度相关
// ——恰好是最需要留下痕迹的那种机器。
func TestStartupFailureDirPrefersThenFallsBack(t *testing.T) {
	if got := startupFailureDir(`/cfg`, `/userconfig`); got != `/cfg` {
		t.Fatalf("已解析的配置目录优先，实得 %q", got)
	}
	got := startupFailureDir(``, `/userconfig`)
	if got != filepath.Join(`/userconfig`, "FileDedup") {
		t.Fatalf("回退到 UserConfigDir/FileDedup，实得 %q", got)
	}
	got = startupFailureDir(``, ``)
	if got != filepath.Join(os.TempDir(), "FileDedup") {
		t.Fatalf("两者都拿不到时应落临时目录而不是不落，实得 %q", got)
	}
}

// 接线锚点：main() 必须真的走这条出口，而不是只在包里放了几个没人调的函数。
// 这一格与 M196 抽出 buildAppOptions 同源——运行时的失败路径在测试里走不到（走到就是
// 起一个真窗口），所以把"调用发生过"钉成一句可 grep 的话。
func TestMainWiresStartupFailureLog(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(raw), "func main()")
	if i < 0 {
		t.Fatal("main.go 里找不到 func main()")
	}
	body := string(raw)[i:]
	for _, want := range []string{"writeStartupFailureLog(", "startupFailureDir(", "os.Exit(1)"} {
		if !strings.Contains(body, want) {
			t.Fatalf("main() 未接上 %s（GUI 无终端 ⇒ 只剩 stderr 等于零痕迹）：\n%s", want, body)
		}
	}
	if !strings.Contains(body, "os.Stderr") {
		t.Fatal("stderr 那句要留着：从快捷方式/命令行启动时它是最近的一条反馈")
	}
}
