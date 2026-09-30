// app_lifecycle.go —— 生命周期与进程内务。
//
// M336（2026-09-28 第七轮审查批）：由 app.go 按职责簇拆分而来。
// **方法名与签名一字未改**——Wails 绑定按方法名解析，与所在文件无关，
// 因此这是零行为改动的纯移动：前后端契约、调用方、既有测试都不受影响。
//
// 包级类型/常量/变量与非方法函数仍留在 app.go：它们被多簇共用，
// 拆开只会让「这个类型在哪」变成第二次查找。
package main

import (
	"context"
	"filededup/internal/cache"
	"filededup/internal/history"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
)

// startup Wails 生命周期。
func (a *App) startup(ctx context.Context) {
	a.setCtx(ctx)
	if dir, err := os.UserConfigDir(); err == nil {
		a.cfgDir = filepath.Join(dir, "FileDedup")
	} else if home, herr := os.UserHomeDir(); herr == nil {
		// 极端环境回退：配置目录不可用时退到家目录，避免静默落到（通常只读的）CWD
		a.cfgDir = filepath.Join(home, ".filededup")
	}
	if a.cfgDir != "" {
		_ = os.MkdirAll(a.cfgDir, 0o755)
	}
	// M4：哈希缓存（确证损坏时隔离重建；打开失败不阻塞应用）
	a.openCache()
	// v0.5.0：扫描历史/清理账本（独立 history.db，确证损坏时隔离重建）
	a.openLedger()
	a.emit(a.ctx, "app:ready", AppVersion)
}

// setCtx 保存 Wails 注入的运行时 context。经 a.mu：linux 腿第二实例回调可能
// 早于/并发于本赋值到达（R-根包-1），裸字段读写会被 race detector 判为数据竞争。
func (a *App) setCtx(ctx context.Context) {
	a.mu.Lock()
	a.ctx = ctx
	a.mu.Unlock()
}

// openCache 打开哈希缓存；失败时除 stderr 外**还必须留一条界面提示**（M25）。
//
// 从 startup 里抽出来与 openLedger 同理（见其注释）：startup 会解析真实的
// os.UserConfigDir，单测调用它就会动到用户机器上的 cache.db。
//
// 2026-09-21（M25，04 §6.8.8）：修正前这里只写 stderr。GUI 没有终端 ⇒ 等于没说，
// 而后果不是崩溃而是**永久变慢且无从排查**：缓存没开成，每次扫描都全量重算，
// 用户只知道"这软件越来越慢"。与 M7（账本落账失败只写 stderr）同病。
// 本方法对 a.cch / a.pipe 的直接写在 M9 白名单同档（启动前置，无并发读者）。
func (a *App) openCache() {
	if a.cfgDir == "" {
		return
	}
	dbPath := filepath.Join(a.cfgDir, "cache.db")
	cch, err := cache.Open(dbPath)
	if err != nil {
		// 不去重、不降级退出，只在启动时说一次：功能不受损，受损的是速度。
		fmt.Fprintf(os.Stderr, "[cache] 哈希缓存不可用，本次运行将全量重算: %v (path=%s)\n", err, dbPath)
		a.addStartupNotice(cacheUnavailableNotice(dbPath, err))
		return
	}
	a.cch = cch
	a.pipe = a.pipe.WithCache(cch)
	// M285（2026-09-27 实施批；源头是 W9-9 那半格真机读数）：库打不开已经有 M25 的
	// 提示，但「打开成功、代价是整表作废」这条路原先只写 stderr —— GUI 用户没有终端，
	// 看到的只是"这软件每次扫描都很慢"。与 openLedger 里 M12b 那条同形。
	if q := cch.QuarantinedTo(); q != "" {
		a.addStartupNotice(cacheQuarantineNotice(q))
	}
}

// addStartupNotice 追加一条启动期提示（空串忽略）。
//
// 2026-09-21（M25）：槽位原来是"后写覆盖先写"，于是两个启动期问题同时发生时，
// 界面只显示后一个（登记原文的次生问题）。改为累积：先写的不再被挤掉，
// GetStartupNotice 的绑定形状与前端消费方式都不变（多条以 \n 分隔）。
// 调用点是"启动前置"（startup → openCache/openLedger），与既有写者同档。
func (a *App) addStartupNotice(msg string) {
	if msg == "" {
		return
	}
	a.mu.Lock()
	if a.startupNotice == "" {
		a.startupNotice = msg
	} else {
		a.startupNotice += "\n" + msg
	}
	a.mu.Unlock()
}

// openLedger 打开账本库，并在"确证损坏→隔离重建"发生时把原因留给界面。
//
// 从 startup 里抽出来是为了可测：startup 会去解析真实的 os.UserConfigDir，
// 单测调用它就会动到用户机器上的 history.db（隔离逻辑甚至会把它改名）。
// 这里对 a.hist 的直接写在 M9 的白名单内（startup / openLedger / shutdown
// 同属"启动前置"，那时还没有任何并发读者）。
func (a *App) openLedger() {
	if a.cfgDir == "" {
		return
	}
	histPath := filepath.Join(a.cfgDir, "history.db")
	hs, err := history.Open(histPath)
	if err != nil {
		// 2026-09-18 审查 C3 之后这里不再只是"失去历史/回撤能力"：账本不可用时
		// 回收站/移动/硬链接会被拒绝执行（仅永久删除照常），见 beginJournal。
		// M43（2026-09-21 审查 §14 兑现）：stderr 那一条留着给 CLI/无终端场景，
		// 但 GUI 用户没有终端——后果必须同时进 startupNotice，否则用户只看到
		// "清理莫名被拒绝"而不知所以然。不用 emit：此刻前端监听器还没注册（同 M12b）。
		fmt.Fprintf(os.Stderr, "[history] 历史库不可用：本次运行不保存历史，且回收站/移动/硬链接清理将被拒绝执行: %v (path=%s)\n", err, histPath)
		a.addStartupNotice(ledgerUnavailableNotice(histPath, err))
		return
	}
	a.hist = hs
	// M12b（2026-09-21 全仓审计 §五 12）：确证损坏的账本被隔离重建后，原先只
	// fprintf(stderr)，GUI 用户没有终端，看到的只是"历史记录页凭空变空"。
	// 不能用 emit：此刻前端的监听器还没注册（bind 在 store.init 里，早于挂载的
	// 事件都会丢），所以存进 startupNotice，由前端初始拉取取走。
	if q := hs.QuarantinedTo(); q != "" {
		a.addStartupNotice(ledgerQuarantineNotice(q))
	}
}

// beforeClose 挂到 options.OnBeforeClose：返回 true 表示「这一次先别关窗口」。
// 命名小写与 startup/shutdown 同列——生命周期钩子不该出现在前端绑定面里。
//
// 2026-09-18 审查 C5：扫描/清理跑在 goroutine 里，而关窗口即进程退出，原先
// 毫无拦截——移动被拦腰截断、写前账本停在 planned、句柄被在途 goroutine 继续
// 使用。交互语义：
//   - 第一次关闭：拦下，请求中止在途任务，发 app:quit-blocked 让前端提示；
//   - 第二次关闭（任务仍未收口）：认定用户就是要走，立即结束进程。
//
// 硬退出是可接受的而非理想：账本本来就是写前的，未落账条目留在 planned，
// 下次启动 history.Open 统一标为 interrupted，用户看得到、可追溯。
// 不无限等待是因为一次卡住的回收站/网络卷调用会让窗口永远关不掉。
func (a *App) beforeClose(ctx context.Context) bool {
	a.mu.Lock()
	if !a.opsRunning && !a.scanInFlight {
		a.quitPending = 0
		a.mu.Unlock()
		return false
	}
	running := "scan"
	if a.opsRunning {
		running = "ops"
	}
	a.quitPending++
	attempt := a.quitPending
	a.mu.Unlock()

	if attempt >= 2 {
		fmt.Fprintf(os.Stderr, "[app] 第二次关闭：在途任务仍未收口，立即退出（未完成条目由下次启动标记为中断）\n")
		a.forceExit(0)
		return true // forceExit 被测试替换时可达
	}
	a.cancelInFlight()
	msg := "清理进行中，已请求中止——待其停下后再点一次即可退出（中止后已完成的部分不会回滚）"
	if running == "scan" {
		msg = "扫描进行中，已请求中止——待其停下后再点一次即可退出（扫描不涉及文件改动）"
	}
	if a.emit != nil {
		a.emit(ctx, "app:quit-blocked", map[string]string{"message": msg, "running": running})
	}
	return true
}

// cancelInFlight 请求中止在途扫描与清理（幂等；不等待收口）。
func (a *App) cancelInFlight() {
	a.mu.Lock()
	cancel := a.opsCancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	_ = a.pipe.Cancel()
}

// shutdown Wails 生命周期：释放缓存与历史库句柄。
//
// 2026-09-18 审查 C5：句柄必须在在途 goroutine 收口后才关——扫描收尾写
// SaveScan、清理收尾写 FinishItem/FinalizeOp，先关句柄会让这些落账变成
// "sql: database is closed"，用户侧表现为历史/账本凭空少一条。
// 等待设上限：单条文件系统调用（网络卷、回收站服务）不可中断，
// 不能让"关不掉"成为代价；超时后按现状放行，并在 stderr 留痕。
func (a *App) shutdown(ctx context.Context) {
	a.cancelInFlight()
	if !waitGroupTimeout(&a.wg, inflightDrainGrace) {
		// APP-3：同样走统一出口。这一条说的直接就是"本次落账可能缺失"，
		// 与 M7 那四处是同一件事，没有理由只留在看不见的 stderr 里。
		a.warnLedger(fmt.Sprintf("在途任务未在 %s 内收口，句柄先行释放（本次落账可能缺失，历史记录未必完整）", inflightDrainGrace))
	}
	// M374：外部定位命令的 waiter 另立一个短上限。这里等的是"那行非零退出的留痕
	// 有没有写出来"，与账本无关，所以超时走 warnBackground 而不是 warnLedger
	// （复用 warnLedger 会造出"一条 Finder 卡住被报成账本写入失败"那种假话）。
	// 超时**不拦退出**：一条挂住的桌面程序不该把关窗口无限期拖住。
	if !waitGroupTimeout(&a.cmdWg, cmdDrainGrace) {
		a.warnBackground("reveal", fmt.Sprintf("外部定位命令未在 %s 内退出（子进程仍在回收，不拦退出）", cmdDrainGrace))
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cch != nil {
		_ = a.cch.Close()
		a.cch = nil
	}
	if a.hist != nil {
		_ = a.hist.Close()
		a.hist = nil
	}
}

// recoverGoroutine P3：绑定层 goroutine 的兜底 panic 守卫。
//
// 扫描/清理跑在独立 goroutine 里，任何 panic（越界、nil map、第三方解码器
// 遇到畸形文件）都会直接带走整个进程——用户看到的现象是"点了扫描，软件消失"，
// 而且未清理的在途标志会让重启前的会话一直显示"扫描中"。
// 这里吞掉 panic 但把堆栈写到 stderr 留痕，并向前端发一条错误事件；
// 状态复位交由 goTask 中先前注册的 reset 完成。
func (a *App) recoverGoroutine(kind string) {
	if r := recover(); r != nil {
		fmt.Fprintf(os.Stderr, "[panic] %s goroutine 已恢复: %v\n%s\n", kind, r, debug.Stack())
		if kind != "ops" {
			a.pipe.Abort() // 状态机停在运行态 → 之后所有扫描都会被"任务进行中"拒绝
		}
		// M202：原句（含 kind 与 panic 值）整串降级为 detail，外壳走 panic 专用档。
		msg := errorEvent(fmt.Errorf("%s goroutine panic: %v", kind, r))
		if a.emit != nil && a.ctx != nil {
			if kind == "ops" {
				a.emit(a.ctx, "ops:error", msg)
			} else {
				a.emit(a.ctx, "scan:error", msg)
			}
		}
	}
}

// goTask 启动绑定层后台任务（P3）：统一挂上 panic 兜底与在途标志复位。
// defer 为 LIFO：先注册 recover（最外层兜底）、后注册 reset，
// 因此 panic 展开时先复位在途标志、再吞掉 panic 发错误事件——
// 前端收到 error 时应用已确定处于"空闲"，不会再看到"扫描中"的残影。
func (a *App) goTask(kind string, reset, body func()) {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done() // 最先注册 → 最后执行：panic 路径也不会漏记账
		defer a.recoverGoroutine(kind)
		defer reset()
		body()
	}()
}

// settingsPath 给出 settings.json 的路径；配置目录不可用时以错误收口。
//
// M60（04 §6.11 APP-12）：cfgDir 在 startup 取不到用户配置目录时留空串
// （app.go:278-288）。改前无条件 filepath.Join(a.cfgDir, "settings.json")，
// 空串时返回的是**相对路径** "settings.json" ⇒ 配置被写进进程 CWD。
// GUI 打包后 CWD 通常是只读目录或 "/"：写失败静默，而读又会把同目录下
// **别的程序**留下的同名文件当成本应用的用户配置（实测：CWD 里放一份
// {"theme":"dark"} 就会被读回来）。openCache:305 / openLedger:365 同档都有
// `if a.cfgDir == ""` 的兜底，唯独这一条漏了。
func (a *App) settingsPath() (string, error) {
	if a.cfgDir == "" {
		return "", fmt.Errorf("配置目录不可用（拿不到用户配置目录），本次不读写 settings.json")
	}
	return filepath.Join(a.cfgDir, "settings.json"), nil
}
