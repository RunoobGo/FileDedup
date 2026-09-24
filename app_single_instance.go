// app_single_instance.go M196（登记表 APP-23）：OS 级单实例保护 + 第二实例聚焦已有窗口。
//
// 登记行原话里的危害是"跨实例一格都不设防"：`opsRunning` 互斥门、写前账本、XDG guard
// 全是进程内防线，两个实例同时把清理打在重叠文件上时两边各自都"正确"，合起来就是重复移动。
// 用户裁定（2026-09-24 七条裁定之一）取"第二实例聚焦已有窗口"这一案
// （另两案"拒绝启动""明告放行"未选）。
//
// 设计段：docs/superpowers/specs/2026-09-24-single-instance-m196-design.md
// 该段 §1 是对 Wails **v2.16.0**（go.mod:7 钉死）的源码核对，本文件的每一处形状都出自它：
//
//   - 第二实例由 Wails 自己带走（darwin/windows `os.Exit(0)`、linux `os.Exit(1)`），
//     我们的代码只在**首实例**侧跑 ⇒ 这里没有任何"要不要退出"的分支可写。
//   - ★ v2.16 公开面**没有** WindowSetFocus/BringToFront，"前置"必须两个原语拼：
//     linux 的 `gtk_window_present` 只挂在 WindowUnminimise 上（linux/window.c:440），
//     darwin 的 makeKeyAndOrderFront+activate 与 windows 的 SetForegroundWindow
//     只挂在 WindowShow 上（darwin/WailsContext.m:427、windows/frontend.go:1036）。
//     单调哪一个都有一条腿落空 ⇒ raiseMainWindow 两个都调，且这个"两个"由
//     app_single_instance_m196_test.go 的 P-2 钉住（否则 §1.3 那张表只是注释）。
//   - 回调跑在 `startSecondInstanceProcessor` 那条**非主线程**协程上；三平台的 Show /
//     Unminimise 内部各自切主线程（ON_MAIN_THREAD / ExecuteOnMainThread / mainWindow.Invoke），
//     所以这里不需要、也不该自己再加主线程跳板。
//   - ★ linux 腿顺序相反：先起 OnStartup 协程（linux/frontend.go:295）再抢锁（:301），
//     故回调可能早于 a.ctx 赋值 ⇒ 下面的 ctx 零值守卫不是防御性摆设：wruntime 拿不到
//     前端时走的是 `log.Fatalf`（pkg/runtime/runtime.go:16-20,25-27），没守卫等于
//     首实例被自己的第二实例处理器打死。
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2/pkg/options"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// singleInstanceUniqueID 是三腿共用的锁标识，各自拼成：
// dbus 名 org.wails_app_FileDedup.SingleInstance、flock 文件 $TMPDIR/FileDedup.lock、
// 互斥体/窗口名 wails-app-FileDedup{sim,sic,siw}。⇒ 不能含 '/'、空格、前导点。
// 与 wails.json 的 productName 同源（都是 FileDedup），换产品名时这里要一起看。
const singleInstanceUniqueID = "FileDedup"

// 两个原生前置原语做成包级变量，是为了让测试能记录 raiseMainWindow **真身**的
// 调用集合与顺序（P-2）。做成 App 字段的话，测试只能证明"回调调了字段"，
// 证明不了本体有没有漏掉其中一个——而"漏掉哪个"恰好是一条腿失去前置能力的全部后果。
var (
	windowUnminimise = wruntime.WindowUnminimise
	windowShow       = wruntime.WindowShow
)

// raiseMainWindow 把已有窗口带到前台并取焦（三腿并集，见文件头 §1.3 那张表）。
// 顺序取"先 Unminimise 后 Show"，这是两原语在任何一腿上都不互相抵消的排法。
// ★ 本函数不声称自己知道某条腿上"只调一个会怎样"的 AppKit/Win32 细节。被钉住的
// 判据只有两条：两个原语都必须被调（少一个就有一条腿失去前置能力，坐标见文件头），
// 以及顺序稳定（P-2）。
func raiseMainWindow(ctx context.Context) {
	windowUnminimise(ctx)
	windowShow(ctx)
}

// onSecondInstance 首实例收到"又有人启动"时要做的事：前置窗口 + 通知前端。
//
// 顺序是刻意的：**先 raise 后 emit**——提示要落在已经被带到前面的那个窗口上。
// 载荷原样交回（不加工、不丢）：前端只拿它做展示，不参与任何判据。
func (a *App) onSecondInstance(data options.SecondInstanceData) {
	if a.ctx == nil {
		// 启动尚未完成（linux 腿的那一格）。这里**不能**碰 wruntime。
		fmt.Fprintf(os.Stderr, "FileDedup: 启动未完成时收到第二实例信号，已忽略（args=%q cwd=%q）\n",
			data.Args, data.WorkingDirectory)
		return
	}
	raiseMainWindow(a.ctx)
	a.emit(a.ctx, "app:second-instance", map[string]any{
		"args":             data.Args,
		"workingDirectory": data.WorkingDirectory,
	})
}

// singleInstanceLock 交给 buildAppOptions 装配。
//
// 不在此处判"锁没抢到"：三腿都是 Wails 内部 os.Exit，我们的代码只在首实例侧被调到。
func (a *App) singleInstanceLock() *options.SingleInstanceLock {
	return &options.SingleInstanceLock{
		UniqueId:               singleInstanceUniqueID,
		OnSecondInstanceLaunch: a.onSecondInstance,
	}
}
