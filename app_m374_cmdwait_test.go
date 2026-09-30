package main

// M374：外部定位命令（Finder / explorer / xdg-open）的 waiter 计入关闭排空
// （2026-09-30 第八轮批 3 / 设计段 §3.2 第七条）。
//
// 现场：`startCmd` 起完子进程就 `go cmd.Wait()`，那条 waiter 从不计入任何集合
// ⇒ `shutdown` 只等 `a.wg`，而 waiter 里还挂着 `warnRevealExit`（"点一下没反应"
// 的留痕）。窗口关掉时进程可以正好死在 waiter 跑出留痕之前 —— 现象就是
// "失败一闪而过、什么都没有留下"。改法：waiter 挂进 App 自己的 `cmdWg`，`shutdown`
// 在排完 `a.wg` 之后用**独立的短上限** `cmdDrainGrace` 排它。
//
// ★ 为什么不并进 `a.wg`：那一格的 10 秒上限与"本次落账可能缺失"这句告警绑定，
// 而一条挂住的 Finder 既不该把关闭窗口拖到 10 秒，也不该触发一句**关于账本**的告警。
// 本文件的第二条用例正是这一条分界（超时后必须放行、且留痕说的不是账本）。

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// blockingCmd 起一个"先活一会儿、再以非零码退出"的子进程。
//
// 用**测试二进制自己**当子进程（同 `exitCodeCmd` 的既有手法，见 app_m58_m60_m61_test.go）：
// 三条腿行为一致，退出码固定 3 ⇒ onExit 一定会被调用，"排空是否覆盖 onExit"才有观测面。
//
// ★ M374 复批（2026-09-30，CI windows 腿首红）：首版按平台选 `sh -c 'sleep…'` /
// `cmd /c "ping -n 1 -w N …"`，而 Windows 上回环**立即应答**、`-w` 只管应答超时
// ⇒ 子进程秒退，本机 darwin 的 `sleep` 恰好盖住了这个差异；windows 腿于是落在
// 「超时放行却完全静默」（子进程早退、`cmdWg` 已在 30ms 前排空）。改成测试二进制桩后，
// 睡眠时长由环境变量给，与平台无关。
func blockingCmd(t *testing.T, d time.Duration) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperExitCode$", "-test.timeout=60s")
	cmd.Env = append(os.Environ(),
		"TEST_HELPER_EXIT=3",
		"TEST_HELPER_SLEEP="+d.String())
	return cmd
}

// 正常排空这一格：shutdown 必须等到 waiter（含它调用的 onExit）跑完才返回，
// 且这一档**不许**留痕（排空成功却说"没排完"是假话）。
func TestM374ShutdownDrainsRevealWaiter(t *testing.T) {
	a, _ := newHistApp(t)
	oldGrace := cmdDrainGrace
	cmdDrainGrace = 5 * time.Second
	defer func() { cmdDrainGrace = oldGrace }()

	var exited atomic.Bool
	if err := startCmd(a, blockingCmd(t, 300*time.Millisecond), func(error) { exited.Store(true) }); err != nil {
		t.Fatalf("夹具子进程起不来: %v", err)
	}
	// 前提自检：这一刻必须真的有一条在途 waiter，否则本条什么都没测到
	// （改前 cmdWg 不存在的那版形状在这里就会露馅）。
	if waitGroupTimeout(&a.cmdWg, 10*time.Millisecond) {
		t.Fatal("夹具前提走样：子进程还活着，cmdWg 却已排空 ⇒ waiter 没被记账")
	}
	logged := captureStderr(t, func() { a.shutdown(context.Background()) })
	if !exited.Load() {
		t.Fatal("M374：shutdown 返回时 waiter 的 onExit 还没跑完 ⇒ 非零退出的留痕会被关窗口吞掉")
	}
	if logged != "" {
		t.Errorf("M374：排空成功却留了痕（实测 [%s]）——「未在…内退出」这句必须是超时专用", logged)
	}
}

// 超时这一格：子进程还活着时 shutdown 不许被拖住，超时走 warnBackground 留痕
// （双通道），且这句话说的**不是账本**（与 a.wg 那 10 秒的分界）。
func TestM374ShutdownGivesUpOnHungCmdWithoutBlockingExit(t *testing.T) {
	a, rec := newHistApp(t)
	oldGrace := cmdDrainGrace
	cmdDrainGrace = 30 * time.Millisecond
	defer func() { cmdDrainGrace = oldGrace }()

	if err := startCmd(a, blockingCmd(t, 2*time.Second), nil); err != nil {
		t.Fatalf("夹具子进程起不来: %v", err)
	}
	if waitGroupTimeout(&a.cmdWg, 10*time.Millisecond) {
		t.Fatal("夹具前提走样：子进程还活着，cmdWg 却已排空")
	}
	start := time.Now()
	logged := captureStderr(t, func() { a.shutdown(context.Background()) })
	elapsed := time.Since(start)
	// 上限 30ms，给足调度余量；真被 2 秒的子进程拖住必然远超这个数
	if elapsed > time.Second {
		t.Fatalf("M374：shutdown 等了 %v（上限 %v）⇒ 一条挂住的定位命令会拖住关窗口", elapsed, cmdDrainGrace)
	}
	if logged == "" {
		t.Error("M374：超时放行却完全静默（stderr 腿丢了）")
	}
	if strings.Contains(logged, "账本") || strings.Contains(logged, "[history]") {
		t.Errorf("M374：超时留痕说成了账本的事（与 a.wg 那句混了）: [%s]", logged)
	}
	if !rec.has("app:error") {
		t.Errorf("打包后的 GUI 没有控制台，只写 stderr 等于没写（事件名：%v）", rec.names())
	}
}
