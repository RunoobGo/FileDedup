package main

// §19 第八批 app 面探针（设计段 §19.3 的 P-19-2 / P-19-2b / P-19-2c / P-19-3 / P-19-4）。
//
// P-19-3 / P-19-4 只引用**改前就存在**的符号（NewApp/SaveSettings/GetSettings/
// ExecuteOperation/history.Store.ListOps），所以在修复落地前必须能编译并且必须红
// （改前真读数已抄进 §19.5）。
//
// P-19-2 / P-19-2b 走的是新签名 startCmd(cmd, onExit)：改前树编译不过，
// 其"修前必红"由变异 M19-b（回调改回 `_ = cmd.Wait()`）提供（见 §19.4）。
//
// M60（APP-12）：cfgDir 取不到时 startup 留空串，openCache/openLedger 各自有
//
//	if a.cfgDir == "" { return }
//
// 的兜底，唯独 settings 没有 ⇒ filepath.Join("", "settings.json") 是**相对路径**，
// 于是配置被写进进程 CWD。GUI 打包后 CWD 常是只读目录或 "/"，写失败静默、
// 下次读又可能读到**别人的** settings.json（同 CWD 的另一个程序留下的）。
//
// M61（APP-13）：S4「永久删除须显式确认」只在执行器里，而 app 层的写前账本
// （beginJournal → BeginOp）在它**上游**。delete 走 undoable=false 那一档，
// beginJournal 不会因为账本问题拒绝 ⇒ 未确认的删除照样在账本里留下一条
// "什么都没动"的 op_records（终态 cancelled），并走完 ops:done 横幅。

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"filededup/internal/model"
)

// emptyCfgDirApp 造一个「配置目录取不到」的 App（复刻 app.go:278-288 的那一档），
// 并把 CWD 换到临时目录，免得探针把 settings.json 写进仓库。
func emptyCfgDirApp(t *testing.T) *App {
	t.Helper()
	a := NewApp()
	a.ctx = context.Background()
	rec := &eventRecorder{done: make(chan string, 8)}
	a.emit = rec.emit
	a.cfgDir = ""
	return a
}

// TestSaveSettingsWithoutCfgDirFailsAndWritesNothing P-19-3（M60 写侧）。
func TestSaveSettingsWithoutCfgDirFailsAndWritesNothing(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	a := emptyCfgDirApp(t)

	if _, err := a.SaveSettings(Settings{Threads: 1, Theme: "dark", Language: "en"}); err == nil {
		t.Errorf("cfgDir 为空时 SaveSettings 必须以错误收口，实测 err == nil（配置被静默写到别处）")
	}
	if _, err := os.Stat(filepath.Join(cwd, "settings.json")); !os.IsNotExist(err) {
		t.Errorf("cfgDir 为空时磁盘上不得出现 settings.json，实测 CWD 里有（err=%v）——那正是写进进程 CWD 的证据", err)
	}
}

// TestGetSettingsWithoutCfgDirIgnoresCwdFile P-19-3 的第二格（M60 读侧）。
//
// 读侧的假话形状和写侧不同：CWD 里只要有一个同名文件（前一条 bug 留下的、
// 或同目录下别的程序写的），GetSettings 就会把它当成本应用的用户配置读回来。
func TestGetSettingsWithoutCfgDirIgnoresCwdFile(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "settings.json"),
		[]byte(`{"threads":4,"theme":"dark","language":"en"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)
	a := emptyCfgDirApp(t)

	if got := a.GetSettings(); !reflect.DeepEqual(got, defaultSettings()) {
		t.Errorf("cfgDir 为空时 GetSettings 必须只给纯默认值，实测 %+v（读到的是 CWD 里那份不属于本应用的配置）", got)
	}
}

// TestUnconfirmedDeleteLeavesNoLedgerRow P-19-4（M61）。
//
// 三格分别钉 §19.1-4 的三个后果：账本不增行、不发 ops:done、opsRunning 不留痕。
// 第二格用"等 2 秒看它发不发"而不是查布尔：断言的是**不会发**，
// 必须给改前形态充分的时间去发，否则这条断言可能只是"还没来得及发"的假绿。
func TestUnconfirmedDeleteLeavesNoLedgerRow(t *testing.T) {
	a, rec, _ := linkedHistApp(t)

	before, err := a.hist.ListOps()
	if err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	var sel []uint64
	for _, f := range a.groups[0].Files {
		sel = append(sel, f.ID)
	}
	a.mu.Unlock()

	opID, gotErr := a.ExecuteOperation(model.OpRequest{Kind: "delete", FileIDs: sel})
	if gotErr == nil {
		t.Errorf("未确认的永久删除必须在落账之前就被拒绝，实测返回 opID=%q err=nil", opID)
	}

	after, err := a.hist.ListOps()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Errorf("账本 op_records 从 %d 涨到 %d：一条「什么都没动」的删除记录留在了历史里，用户会以为发生过一次可回撤的操作",
			len(before), len(after))
	}

	select {
	case ev := <-rec.done:
		t.Errorf("未确认的删除仍然走完了派发并发了终止事件 %q（序列 %v）", ev, rec.names())
	case <-time.After(2 * time.Second):
	}

	a.mu.Lock()
	running := a.opsRunning
	a.mu.Unlock()
	if running {
		t.Errorf("拒绝路径没复位 opsRunning，此后所有清理都会永久返回「操作执行中」（P2 死锁形状）")
	}
}

// ---------- M58（APP-8）：外部定位命令的退出状态不得整个丢掉 ----------

// TestHelperExitCode 是给下面两个用例当**子进程**用的桩，不是探针：
// 只有 TEST_HELPER_EXIT 置位时它才按指定退出码结束；正常跑套件时直接返回。
// （与标准库 os/exec 自带用例同法。）
func TestHelperExitCode(t *testing.T) {
	code := os.Getenv("TEST_HELPER_EXIT")
	if code == "" {
		return
	}
	n, err := strconv.Atoi(code)
	if err != nil {
		t.Fatalf("TEST_HELPER_EXIT=%q 不是退出码", code)
	}
	os.Exit(n)
}

// exitCodeCmd 拉起"启动必然成功、随即以给定退出码结束"的子进程。
//
// 为什么不用 `/bin/sh -c 'exit 3'`：那是假设 POSIX shell 存在，Windows 分支拿不到
// 读数；测试二进制自己是最可控、跨平台的桩。
func exitCodeCmd(code string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperExitCode$", "-test.timeout=30s")
	cmd.Env = append(os.Environ(), "TEST_HELPER_EXIT="+code)
	return cmd
}

// TestStartCmdReportsNonZeroExit P-19-2 主格：Start 成功、子进程随即非零退出时，
// 退出状态必须经回调交回调用方（改前这里是 `_ = cmd.Wait()`，整个丢掉，
// 现象就是"点一下没任何反应"）。
func TestStartCmdReportsNonZeroExit(t *testing.T) {
	reported := make(chan error, 1)
	if err := startCmd(exitCodeCmd("3"), func(werr error) { reported <- werr }); err != nil {
		t.Fatalf("子进程启动失败，前提塌了：%v", err)
	}
	select {
	case werr := <-reported:
		if werr == nil {
			t.Errorf("退出码 3 上报的是 nil error，调用方拿不到任何原因")
		} else if !strings.Contains(werr.Error(), "exit status 3") {
			t.Errorf("上报内容应能读出退出码，实测 %q", werr)
		}
	case <-time.After(10 * time.Second):
		t.Error("子进程已以退出码 3 结束，但退出回调一次都没被调用——界面仍是「点一下没反应」")
	}
}

// TestStartCmdStartFailureUsesReturnOnly P-19-2 第二格：Start 本身失败仍只走返回值，
// 回调不得再报一次（两条通道重复报同一件事 = 同一个失败弹两条提示）。
func TestStartCmdStartFailureUsesReturnOnly(t *testing.T) {
	reported := make(chan error, 1)
	cmd := exec.Command(filepath.Join(t.TempDir(), "no-such-file-manager"))
	if err := startCmd(cmd, func(werr error) { reported <- werr }); err == nil {
		t.Error("不存在的可执行文件必须由 Start 返回错误，实测 err == nil")
	}
	select {
	case werr := <-reported:
		t.Errorf("Start 失败已走返回值，回调不得再报一次：%v", werr)
	case <-time.After(500 * time.Millisecond):
	}
}

// TestStartCmdQuietOnSuccess P-19-2b（反面钉）：退出码 0 时一次都不许多报，
// 否则每打开一次文件夹就弹一条错误提示。
//
// 断言的是**不会调用**，所以必须给足时间让任何误报落地；2s 相对"子进程 ms 级退出"
// 已是三个数量级的余量。
func TestStartCmdQuietOnSuccess(t *testing.T) {
	reported := make(chan error, 4)
	if err := startCmd(exitCodeCmd("0"), func(werr error) { reported <- werr }); err != nil {
		t.Fatalf("子进程启动失败，前提塌了：%v", err)
	}
	select {
	case werr := <-reported:
		t.Errorf("退出码 0 也上报了失败：%v", werr)
	case <-time.After(2 * time.Second):
	}
}

// TestWarnBackgroundKeepsLedgerPrefixOutOfSharedChannel P-19-2c：M58 抽共用出口时
// 最容易写错的那一格——"账本写入失败："是**调用方的话**，不是出口的话。
// 写死在出口上会把一条 Finder 启动失败报成账本故障（假话）；
// 反过来 warnLedger 丢了这个前缀则是把 M7 的口径改瘦了。
func TestWarnBackgroundKeepsLedgerPrefixOutOfSharedChannel(t *testing.T) {
	rec := &eventRecorder{done: make(chan string, 4)}
	a := NewApp()
	a.ctx = context.Background()
	a.emit = rec.emit

	const revealMsg = "打开所在文件夹失败（/tmp/x）：exit status 1"
	a.warnBackground("reveal", revealMsg)
	if !rec.has("app:error") {
		t.Fatal("warnBackground 没发 app:error：打包后的 GUI 没有控制台，只写 stderr 等于没写")
	}
	if got := ledgerErrText(t, rec); got != revealMsg {
		t.Errorf("界面收到的必须是与 stderr 同一句话，实得 %q", got)
	}

	a.warnLedger("回撤失败态落库出错")
	if got := ledgerErrText(t, rec); !strings.HasPrefix(got, "账本写入失败：") {
		t.Errorf("warnLedger 一档必须保留「账本写入失败：」前缀（M7 口径），实得 %q", got)
	}
}
