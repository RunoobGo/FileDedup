package main

// M288（04 §6.50 W4-5 真机定案）：Windows 上 `explorer.exe` **成功也返回 1**，
// 而 M58 给 startCmd 加的 onExit 出口把任何非零退出都报成失败 ⇒ 四个定位动作
// 真机误弹 4/4（窗口确实开起来了，stderr 四条全是 `exit status 1`）。
//
// 修法必须**同时**满足两侧（M288 行内取向原话）：
//   - 假成功不报错：explorer 的退出码在这一通道上不含信息 ⇒ 不弹 error toast；
//   - 真失败看得见：其余外部命令的非零退出照旧走 warnBackground（M58 那条腿不许回归），
//     explorer 那一支的失败仍在 stderr 留痕（打包后无控制台时是日志，不是静默）。
//
// ★ 三平台可测的做法沿用 APP-6 的先例：**平台真值经参数注入**。
//   于是判据与出口两条臂在 linux CI 上也能各跑一遍，不必 t.Skip；
//   只有"装配出来的命令真的是 explorer"这一条必须 Windows 主机（见最后一条用例）。

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// exitStatusOne 复刻 `cmd.Wait()` 在非零退出时的 error 形状（`ExitError.Error()` 即此串）。
var exitStatusOne = errors.New("exit status 1")

// captureStderr 收走一次调用写到 stderr 的内容（"静默"与"只留日志"这两档的差别
// 只能在出口处验，别的通道都看不见）。
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	fail := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		fail <- string(b)
	}()
	fn()
	_ = w.Close()
	os.Stderr = old
	out := <-fail
	if len(strings.TrimSpace(out)) == 0 {
		return ""
	}
	return out
}

// TestRevealExitSilentOnlyWindowsExplorer 判据表（纯函数，三平台同测）。
func TestRevealExitSilentOnlyWindowsExplorer(t *testing.T) {
	cases := []struct {
		goos string
		exe  string
		want bool
		why  string
	}{
		{"windows", "explorer", true, "LookPath 未跑通时 cmd.Path 仍是裸名"},
		{"windows", `C:\Windows\explorer.exe`, true, "生产形态：解析后的绝对路径"},
		{"windows", `C:\Windows\EXPLORER.EXE\`, true, "Windows 路径末段带尾分隔符仍取到 explorer"},
		{"windows", `C:/Windows/explorer.exe`, true, "Windows 上正斜杠同样是分隔符"},
		{"windows", "EXPLORER.EXE", true, "Windows 大小写不敏感"},
		{"windows", "notepad", false, "只有 explorer 的退出码不作判据"},
		{"linux", "explorer", false, "非 Windows 上这条判据不许生效"},
		{"darwin", "open", false, "darwin 的 open 退出码可信（M58 那条腿的正面）"},
		{"linux", "/usr/bin/xdg-open", false, "linux 探测表里的命令退出码可信"},
	}
	for _, c := range cases {
		if got := revealExitSilent(c.goos, exec.Command(c.exe, "x")); got != c.want {
			t.Errorf("revealExitSilent(%q, %q) = %v，应为 %v（%s）", c.goos, c.exe, got, c.want, c.why)
		}
	}
}

// TestWarnRevealExitExplorerNoiseIsSilentButLogged 出口：explorer 那一支不弹 toast、
// 但必须在 stderr 留痕——"看不见"与"没写"是两件事，真失败时还得能从日志里查。
func TestWarnRevealExitExplorerNoiseIsSilentButLogged(t *testing.T) {
	a, rec := newHistApp(t)
	logged := captureStderr(t, func() {
		a.warnRevealExit("windows", exec.Command("explorer", `E:\回收站`), "trash", "打开系统回收站", exitStatusOne)
	})
	if rec.has("app:error") {
		t.Errorf("explorer 的正常非零退出仍弹了 error toast（事件名：%v）——M288 的『狼来了』形状", rec.names())
	}
	if logged == "" {
		t.Errorf("静默那一支必须至少在 stderr 留一行痕，实测空")
	}
}

// TestWarnRevealExitOtherCommandStillWarns 反例（M58 不许回归）：非 explorer 命令的
// 非零退出照旧双通道可见——"为了让 explorer 闭嘴"把整条出口哑掉就是修一半。
func TestWarnRevealExitOtherCommandStillWarns(t *testing.T) {
	a, rec := newHistApp(t)
	subject := "打开所在文件夹（E:\\a.bin）"
	logged := captureStderr(t, func() {
		a.warnRevealExit("linux", exec.Command("xdg-open", "/tmp/x"), "reveal", subject, exitStatusOne)
	})
	if !rec.has("app:error") {
		t.Errorf("非 explorer 命令的失败必须发 app:error，实测事件名：%v", rec.names())
	}
	ev, ok := rec.lastWith("app:error")
	if !ok {
		t.Fatal("app:error 没有载荷")
	}
	msg, _ := ev.(map[string]string)["error"]
	if msg == "" {
		t.Fatalf("app:error 载荷缺 error 正文：%+v", ev)
	}
	if logged == "" {
		t.Errorf("warnBackground 的 stderr 腿丢了")
	}
}

// TestWarnRevealExitNilErrorIsNoop 反面钉（P-19-2b 同族）：退出码 0 时一次都不报。
func TestWarnRevealExitNilErrorIsNoop(t *testing.T) {
	a, rec := newHistApp(t)
	logged := captureStderr(t, func() {
		a.warnRevealExit("windows", exec.Command("explorer", "x"), "reveal", "打开所在文件夹", nil)
		a.warnRevealExit("linux", exec.Command("xdg-open", "x"), "open", "打开", nil)
	})
	if rec.has("app:error") {
		t.Errorf("成功退出不得报失败（事件名：%v）", rec.names())
	}
	if logged != "" {
		t.Errorf("成功退出不得留失败痕，实测：[%s]", logged)
	}
}

// TestRevealPathWindowsExplorerNoiseEndToEnd 端到端：生产装配链上（RevealPath →
// revealCmd → execRevealCmd → onExit）Windows 真机那一次误弹不再复现。
// ★ 只有 Windows 主机上 revealCmd 装配的才是 explorer；其余平台由上面三条注入 goos
//
//	的用例覆盖同一判据，这里必须 SKIP 而不是"换个命令假装测过"（AS-K2）。
func TestRevealPathWindowsExplorerNoiseEndToEnd(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("非 Windows 主机：revealCmd 的探测表装配不出 explorer，端到端这一臂只能在 Windows 上取")
	}
	a, rec := newHistApp(t)
	dir := t.TempDir()
	f := filepath.Join(dir, "keep.bin")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var got []*exec.Cmd
	old := execRevealCmd
	execRevealCmd = func(cmd *exec.Cmd, onExit func(error)) error {
		got = append(got, cmd)
		onExit(exitStatusOne) // 子进程"成功打开窗口后返回 1"
		return nil
	}
	defer func() { execRevealCmd = old }()

	if err := a.RevealPath(f); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("exec 次数 = %d", len(got))
	}
	if base := strings.TrimSuffix(filepath.Base(got[0].Path), ".exe"); !strings.EqualFold(base, "explorer") {
		t.Fatalf("本用例的前提是装配出 explorer，实测 %q", filepath.Base(got[0].Path))
	}
	if rec.has("app:error") {
		t.Errorf("Windows 上 explorer 正常退出被报成失败（事件名：%v）——正是 M288 真机读到的 4/4 误弹", rec.names())
	}
}
