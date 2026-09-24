# M196 设计段：OS 级单实例保护 + 第二实例聚焦已有窗口（2026-09-24）

> 登记行：04 登记表 **M196 / APP-23**（占号 `69e3acc`，来源 2026-09-24 全面审查）。
> 用户裁定（本批七条裁定之一）：**第二实例聚焦已有窗口**（另两案"拒绝启动""明告放行"未选）。
> 登记行原文的两条前置：① 加锁属产品行为决策 ⇒ 已由裁定关闭；② "Wails 的 `SingleInstanceLock`
> 回调语义要先核对该版本（v2.16）三平台行为" ⇒ **本段 §1 就是这份核对**，全部现取模块缓存
> `github.com/wailsapp/wails/v2@v2.16.0`（go.mod:7 钉的就是它），不引任何上游文档记忆。

## 0. 这一批要堵的到底是什么

登记行的危害面是"**跨实例**一格都不设防"：现有 `opsRunning` 互斥、写前账本、XDG guard
全是**进程内**防线。两个实例同时把清理打在重叠文件集上时，两边的账本各自都"正确"，
合起来就是重复移动/重复删除；`history.db`/`cache.db` 也是两个进程同时开。
所以本批的判据不是"有没有窗口动画"，而是**第二个进程必须在动手之前就走掉**。

## 1. v2.16.0 语义核对（现取，逐条给坐标）

### 1.1 契约面

`pkg/options/options.go:190` `SingleInstanceLock{UniqueId string; OnSecondInstanceLaunch func(SecondInstanceData)}`，
挂在 `options.App.SingleInstanceLock`（同文件 :87）；`SecondInstanceData{Args []string; WorkingDirectory string}`（:196）。
**公开面无 `WindowSetFocus`/`BringToFront`**（`pkg/runtime/window.go` 全量 grep `Focus` 零命中）
⇒ "聚焦"只能用现有原语拼，见 §1.3。

### 1.2 三平台各自的机制与退出姿势（第二实例由 Wails 自己带走）

| 腿 | 锁的载体 | 第二实例行为 | 时机 | 降级路径 |
|---|---|---|---|---|
| darwin | `$TMPDIR(原生)<UniqueId>.lock` + `flock LOCK_EX\|LOCK_NB`（`darwin/single_instance.go:25-51,68-86`）| 发通知 + **`os.Exit(0)`** | `Frontend.Run()` **最前面**（`darwin/frontend.go:230-232`），★ **早于 OnStartup**（:249 起协程）| 抢锁失败一律按"已有实例"处理 ⇒ **连"临时目录不可写"这种跟别人无关的失败也会让应用静默不启动** |
| windows | `CreateMutex` + 隐藏消息窗口 + `WM_COPYDATA`（`windows/single_instance.go:37-75`）| **`os.Exit(0)`** | 同样早于 OnStartup（`windows/frontend.go:166-168` vs :229）| `ERROR_ALREADY_EXISTS` 但 `FindWindowW` 拿不到句柄 ⇒ **不退出，照常起第二个实例**（:70 注释自陈）|
| linux | **D-Bus 会话总线**名 `org.wails_app_<id>.SingleInstance`（`linux/single_instance.go:22-76`）| **`os.Exit(1)`**（★ 退出码与另两腿不同）| ★ **晚于 OnStartup 的启动**：:295 先起 `OnStartup` 协程，:301 才 `SetupSingleInstance` | ① `ConnectSessionBus()` 失败 ⇒ **直接 return，零保护**（:28-33）；② `SendMessage` 失败 ⇒ return，**放行第二实例**（:71-74）|

三条必须一起记住的结论：
1. **锁在 Windows/Linux 上不是原子的"要么挡住要么明说"**——两腿各有"挡不住就照常起"的静默放行分支。
   所以本批不许把 M196 报成"跨实例并发已封死"，只能报"三腿尽力 + 两腿有已知的静默放行残余"。
2. **linux 腿的第二实例可能在被带走之前跑到 `startup()` 开头**（`openCache`/`openLedger` 会短暂开库）。
   这一格由 §3 的 P-3 用代码路径覆盖（ctx 未就绪时不得 panic），但**顺序本身在 Wails 里，不能由我们修**。
3. darwin 那条"临时目录不可写 ⇒ 应用静默不启动"是**打开这个开关新引入的可用性风险**，
   登记行里没有。写进手册（§5），并且不假装它不存在。

### 1.3 "聚焦已有窗口"到底调什么（本批最需要读源码、最不能猜的一格）

| 原语 | darwin 实际效果 | windows 实际效果 | linux 实际效果 |
|---|---|---|---|
| `runtime.WindowShow` | `[mainWindow makeKeyAndOrderFront]` + `activateIgnoringOtherApps:YES`（`WailsContext.m:427-430`）＝**真前置+取焦** | `ShowWindow` + `SetForegroundWindow` + `SetFocus`（`frontend.go:1007-1039`）＝**真前置+取焦** | 只 `gtk_widget_show`（`window.c:405-410`）＝**不前置** |
| `runtime.WindowUnminimise` | `deminiaturize`（`WailsContext.m:415-417`）＝已最小化才有效 | `Restore()`（`frontend.go:366-372`）＝已最小化才有效 | **`gtk_window_present`**（`window.c:440-445`）＝**这才是 linux 的前置+聚焦** |

⇒ 单调哪一个都在某一腿上落空：**两个都调**才三腿齐（linux 靠 `Unminimise`，darwin/windows 靠 `Show`，
另一方在另两腿是幂等/无害）。这就是 §2 把两个原语都做成注入依赖、并由测试钉"两个都被调 + 顺序"的理由。

### 1.4 线程安全（回调跑在非主线程）

消费协程是 `go result.startSecondInstanceProcessor()`（darwin `frontend.go:139` / linux :247 / windows :140），
回调因此**不在主线程**。三平台的 Show/Unminimise 内部各自切主线程：
darwin `ON_MAIN_THREAD(...)`（`Application.m:230-236`）、linux `C.ExecuteOnMainThread(...)`（`window.go:239-241`）、
windows `f.mainWindow.Invoke(...)`（`frontend.go:1008`）。⇒ 从回调里直接调这两个原语是安全的，
不需要我们自己再加主线程跳板。**这条是"读源码得到的"，猜的话很可能猜成"要自己 Dispatch"。**

## 2. 改法（五处，全部最小面）

1. **`main.go` 抽装配函数**：`wails.Run(&options.App{...})` 的字面量整段移进
   `func buildAppOptions(app *App) *options.App`，`main()` 只留 `wails.Run(buildAppOptions(app))`。
   纯移动，不加行为。★ 理由：装配契约（"开关真的接上了吗"）必须可断言，否则本批最像 bug 的
   失败模式——**忘接 `SingleInstanceLock`**——没有任何东西能抓住。`//go:embed` 仍留在 var 上不动。
2. **新 `app_single_instance.go`（package main）**：
   - `const singleInstanceUniqueID = "FileDedup"`。三腿各自把它拼成
     `wails_app_FileDedup`（dbus）/ `FileDedup.lock`（flock 文件名）/ `wails-app-FileDedup{siw,sic,sim}`（win），
     三种形态都合法 ⇒ 不含 `/`、空格、前导点。
   - `type raiseWindowFn func(ctx context.Context)` + `App.raise` **可测接缝**（与既有 `emit`/`forceExit` 同族同姿势），
     默认实现 `raiseMainWindow` = 先 `wruntime.WindowUnminimise(ctx)` 后 `wruntime.WindowShow(ctx)`。
   - `func (a *App) singleInstanceLock() *options.SingleInstanceLock`：回调指向 `a.onSecondInstance`。
   - `func (a *App) onSecondInstance(data options.SecondInstanceData)`：
     ★ `a.ctx` 零值 ⇒ 记一行 stderr 后**整体 no-op**（linux 腿 §1.2 结论 2 的那一格；raise 与 emit 都吃 ctx）；
     否则**先 raise 后 emit**（窗口先到位，前端提示才有落点），事件 `app:second-instance`，
     载荷 `{args, workingDirectory}` 原样带（前端不说谎：提示里能给出被合并的那次是哪儿来的）。
3. **`main.go` 接上**：`SingleInstanceLock: app.singleInstanceLock(),`。
4. **前端 `scan.ts` 加一条 `bind('app:second-instance', ...)`** → info toast。
   与既有 `bindEvents` 同一姿势（含 `offEvent` 收口，C12）。载荷只做展示，不参与任何判据。
5. **文档**：09 登记代码事实（三腿机制表 + 两条静默放行残余 + darwin 可用性风险）；
   10 只写用户看得见的那一面（"再开一个窗口会自动回到已打开的那个"，以及 linux 上可能不生效的诚实口径）。

不做（超出裁定的部分，逐项给理由）：
- **不自研第二把锁**（`internal/instlock` 之类）。裁定选的是 Wails 的 `SingleInstanceLock`；
  自研会在同一件事上放两套判据，且真正补不到 Wails 的那两条静默放行（它们在 Wails 内部）。
  linux 无会话总线 ⇒ 零保护这一格**如实登记**，另案。
- **不改 `fdd-cli`**：它只读（`cmd/fdd-cli/main.go` 全量 grep 写操作，唯一命中的是 `cache.Open`），
  不吃 `ops.Execute` ⇒ 不在"两个进程同时把清理打在重叠文件集"的危害面上。
- **不做"第二实例参数转发"**（把 `data.Args` 变成"扫描这个目录"的动作）：本应用无 CLI 参数入口，
  转发一个不存在的语义属造需求。

## 3. 测试与变异（预测集先写下，实测后对账）

新 Go 用例（`app_single_instance_m196_test.go`，package main）：

- **P-1 装配契约**：`buildAppOptions(app).SingleInstanceLock` 非 nil、`UniqueId == singleInstanceUniqueID`、
  `OnSecondInstanceLaunch != nil`，并且**该回调就是 `a.onSecondInstance`**（喂一个 data 进去，断 `raise` 接缝被触发）。
  ⇒ 这一格专杀"忘了接线"和"接了但回调指向别的"。
- **P-2 两个原语都被调 + 顺序**：接缝换成可记录的实现（`unminimise`/`show` 各记一次），
  断**两次都发生**、`Unminimise` 在 `Show` **之前**、各恰好一次。
  ★ 这一格是 §1.3 那张表的**唯一机器可查证据**：表说"linux 的前置只来自 Unminimise"，
  那么"漏调 Unminimise"必须被杀——否则这一格判据就停留在注释里。
- **P-3 ctx 未就绪不得 panic**：`a.ctx` 为零值时直接调回调 ⇒ 断不 panic、不 emit、不 raise
  （linux 腿 §1.2 结论 2 的代码路径；`wruntime.WindowShow(nil ctx)` 会打到 EventsEmit 的
  `log.Fatalf` 同族陷阱上，见 app.go:318-323 那条接缝注释）。
- **P-4 连发两次**：两次回调 ⇒ 两次 raise、两次 emit，事件名恰为 `app:second-instance`，
  载荷 `args`/`workingDirectory` 原样（不说谎：不加工、不丢）。
- **P-5 前端接线**（`frontend/tests/`，与既有 33 条接线断言同族）：`app:second-instance` 被 `bindEvents` 绑定、
  卸载时走 `offEvent`、提示文本含"已打开"。

变异（每条先 `cp` 备份，还原用 `cp`+`diff`；预测集=实测集要对账，差集照记）：

| 变异 | 预测杀 |
|---|---|
| Va `main.go` 删掉 `SingleInstanceLock:` 一行 | P-1 |
| Vb `raiseMainWindow` 去掉 `WindowUnminimise`（只留 Show）| P-2（★ 这是 §1.3 那张表的承重格）|
| Vc 回调体去掉 ctx 零值守卫 | P-3 |
| Vd `emit` 在 `raise` 之前 | **预测：无人可杀 ⇒ 若真如此就照记"顺序判据未测到"**，然后改 P-2/P-4 用同一记录器记下全局次序补上 |
| Ve `UniqueId` 改成含 `/` 的值 | **预测：无人可杀**（三腿合法性是运行时事实）⇒ 若 P-1 只比常量相等，这条就是"无证据"的格子，须补一格形状断言 |

### 3.1 实测对账（2026-09-24 本机现跑，报告 `/tmp/gates-m196-122136.log` 前后）

跑法：`bash` 驱动脚本逐条 `python3` 打点 → `go test -run TestM196 -count=1 ./` → `cp` 还原 + `diff -q` 校验；
不用 `git checkout` 还原（那会连未提交的真实现一起带走）。收尾时两文件与备份 `diff` 均一致。

| 变异 | 预测杀 | **实测杀** | 对账 |
|---|---|---|---|
| Va 删 `SingleInstanceLock:` 装配行 | P-1 | P-1 | 一致 |
| Vb `raiseMainWindow` 只留 Show | P-2 | **P-2 + P-4** | ★ **预测过窄**：P-4 断的是"原语与事件混在同一条序列里"的完整形状，少一个原语它同样倒。这条差集是好事（两格独立钉同一判据），但预测写窄了照记 |
| Vc 去掉 ctx 零值守卫 | P-3 | P-3 | 一致 |
| Vd emit 抢在 raise 之前 | "无人可杀" | **P-4** | ★ **预测过窄**：预测段里已经写了"若真如此就改 P-2/P-4 用同一记录器补上"，实现时提前用了同一个 `seqLog`，所以这一格当场被杀。预测按"写下时的测试形状"记，不因事后实现更强而回改 |
| Ve `UniqueId` 换成 `"FileDedup/sub"` | 预测无人可杀 | **无人可杀（存活）** | 一致 ⇒ 按预测段的规定**补了 P-6**（三腿拼接式的字符交集断言），复跑 Ve 由 P-6 杀掉。★ 补格前后各跑一次，中间状态（"Ve 无人可杀"）不回改 |

★ 另一条**不是变异**的实测：P-4 第一版忘了换掉包级原语，直接打到真 `wruntime`，
得到 `cannot call 'github.com/wailsapp/wails/v2/pkg/runtime.WindowUnminimise': An invalid
context was passed` 且**测试进程被 `log.Fatalf` 带走**（整包 `FAIL`，无 `--- FAIL` 行）。
这一手把 §1.4/P-3 那条"守卫缺失的后果"从推断变成了读数：普通 context 下 wruntime 不是
返回错误，是直接打死进程。

## 4. 本批能被机器证明的、和不能被证明的（不许混）

★ **本批没有"改前当场红"这一格**，且不是漏了：`buildAppOptions` / `singleInstanceLock` /
`windowUnminimise` 全是新符号，拿新用例去跑改前树只会 `undefined: buildAppOptions` 编译失败
——那是"红在错误的格子上"，不能当修前证据（与 §6.34 里 M202 P-5 同一族处理）。
改前的真实状态由**登记行自己的取证**承载：2026-09-24 全仓 grep `SingleInstanceLock`/`flock`/
`LockFileEx`/锁文件 **零命中** ⇒ 确实一格 OS 级保护都没有。可证伪性因此全部落在 §3 的五刀变异上。

**能证明**（本地 + CI）：
- 三腿编译：`go test ./...` 在 ubuntu/windows/macos 三腿都编译 package main ⇒
  `options.SingleInstanceLock` 装配在三腿都编译得过（这是一格真的跨腿证据）。
- P-1~P-5 覆盖的装配、调用集合、顺序、零值守卫、载荷原样、前端接线。

**不能证明 ⇒ 只能写"代码已改、验证未兑现"**：
- **真的双开合并**：需要打包后的 `.app`/`.exe` 起两个 GUI 进程、还要看 dock/任务栏行为。
  CI 三腿都不起 GUI（冒烟走 `fdd-cli`），本机也没有可断言的窗口栈判据 ⇒ 本批**不宣称已验证**。
- **§1.3 那张表的真机效果**（"前置成功"）：机器只证明"两个原语都被调"，
  不证明"调了之后窗口真的到最前"。这一格留给 Windows/macOS 真机清单（05 那份）。
- **linux 无 D-Bus 会话总线时零保护**、**windows `FindWindowW` 失败时放行**：
  这是从 Wails 源码读出来的**事实**（给了坐标），不是我们的实现缺陷，也不是"未验证"——
  口径要分清，不许写成"已通过"，也不许写成"疑似"。

## 5. 用户可见后果（要进手册的两条）

1. 已有一个 FileDedup 时再开一个：**第二个窗口不会出现**，前一个会被带到前台并显示一条提示。
2. 新引入的失败面（darwin）：若系统临时目录不可写，**应用会静默不启动**（Wails 把所有抢锁失败
   一律当"已有实例"）。手册的"打不开"一格要写这条排查项。★ 这条在登记行里没有，是本批读源码新增的。

## 6. 划账阶段追加（2026-09-24，写本段的人与写 §0~§5 的是同一批，但**不回改上文**）

1. §4 "不能证明"那格原文写"留给 Windows/macOS 真机清单（05 那份）"——**当时没核实 05 的范围**。
   现读：`docs/05` 标题、§0.1 状态口径、§0.3 环境要求全是 Windows 语境，本文没有擅自把它扩成
   三腿清单。落地结果：Windows 那半进了 05 新增的 **P10（W11-1~W11-6）**；macOS 的
   "`$TMPDIR` 不可写 ⇒ 静默不启动"与 linux 的"回调早于 `a.ctx`"两格**目前没有清单收录**，
   只有 09 §2.1.1/§10 的排查口径。要么扩 05、要么另立 macOS 清单，**需用户裁定**（已记 04 §6.36 五）。
2. 新增一条**覆盖面边界**（§1 核对时漏了，写 W11 清单才撞出来）：`SetupSingleInstance` 拼的
   互斥体名 `id+"sim"`、类名 `id+"-sic"`、窗口名 `id+"-siw"` **都不带 `Global\` 前缀**，属登录会话
   命名空间；darwin 锁文件在各自 `$TMPDIR`、linux 名字在各自会话总线 ⇒ **三条腿一致地不跨用户**。
   它不同于 §1.2 那两条"静默放行"（那两条要求互斥体先报 `ERROR_ALREADY_EXISTS`，跨会话走不到），
   所以按"边界"记进 09 §2.1.1 与 W11-5，**没有登记成缺陷**，也没有为它加代码。
