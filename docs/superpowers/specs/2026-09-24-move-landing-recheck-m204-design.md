# M204 设计段：move 的落点复审——入口一次性校验改为每次动作前问一次（2026-09-24）

## 0. 任务源与裁定

登记表 **M204/APP-26**（§6.29 批 C 占号）原文取向：

> ✓ 登记待裁定：收口=执行器每次 rename 前按**实际落点**再问一次 `withinDir`（需要把授权集
> 或其快照传进 `op`），属接口面；窗口真被利用需并发第三方，风险与 M48 族同形。
> ★ 与 M184 同批判（同文件同步 syscall 族）可省往返。

**2026-09-24 用户裁定：rename 前按实际落点复审 `withinDir`。**

登记行里"传进 `op`"这一句本批**不按字面做**，理由写在 §2-1：`model.OpRequest` 是前端可控的
值对象，授权依据走它等于把"授权集"变成一个可被请求方填写的字段。改走 `ops.Options`。

## 1. 现状真读数（2026-09-24 现取）

- 校验点**只有一处、只在入口**：`ExecuteOperation` 在持 `a.mu` 时跑
  `if op.Kind == "move" && !a.moveTargetAllowed(op.TargetDir)`，拒绝即返回错误；放行后
  置 `a.opsRunning` 并 `a.goTask("ops", …)` 异步执行。
- 校验与动作之间隔着的是**整个派发窗口**：`opsExecuteFn(ops.Options{…}, op)` →
  `Execute` 的校验阶段（逐文件 `VerifyFile` + 身份记录）→ `case "move"` 的串行循环 →
  `moveFileDetailed(e.Path, op.TargetDir)`。N 个文件的读盘校验全在窗口内。
- `moveFileDetailed` 内部对目标目录**零复审**：`os.MkdirAll(targetDir)` →
  `claimDst(targetDir, base)`（O_EXCL 原子占名，返回 `dst.path`）→ `renameFile(src, dst.path)`，
  跨卷则 `copyVerifyFile` → 删源。执行器只复核**被搬的文件**的身份
  （`guardIdentity`），**从不复问落点归属**——这正是登记行钉的那一格。
- 判据现读一份，都在根包：`resolveTargetPath`（对最近的已存在祖先求 `EvalSymlinks`，
  不存在的尾段原样接回）+ `withinDir`（按路径段边界前缀）+ `moveTargetAllowed`
  （遍历 `a.authDirs` 任一命中即放行）。AS-H3 那笔修的就是"OR 候选放行"，现在只认解析后的真实路径。
- `a.authDirs` 是 `map[string]bool`，`authorizeDir` 在锁内写；`Execute` 跑在另一条 goroutine 上
  ⇒ 执行期直接读这张表既是数据竞争也是**语义错**（用户在操作中途又能选目录，授权集会漂）。

## 2. 修法

1. **快照，而不是活表**：`ExecuteOperation` 在入口校验**通过的同一把锁内**取
   `roots := a.authSnapshotLocked()`（`[]string` 拷贝）。此后整次操作用这份快照 ⇒
   执行期无锁访问、无竞争，且"这一批文件凭什么被允许落地"有唯一时刻的定义。
2. **判据仍只有一份，留在根包**：新增
   `func (a *App) landingGuard(roots []string) func(string) error`——内部就是现有的
   `resolveTargetPath` + `withinDir`，对传入的**实际落点全路径**求解析、任一授权根命中即放行。
   ★ 不把 `withinDir` 抄进 `internal/ops`：两处实现必然漂移，这是 AS-H6 一族点名的形状
   （"判据只有一份才不会再漂移"）。ops 侧只拿一个函数值。
3. **注入面是 `Options`，不是 `OpRequest`**：`ops.Options` 新增
   `MoveLandingAllowed func(landingPath string) error`（nil ⇒ 不复审，与
   `TrashVerifiesRecycle` 同族的"默认保持既有行为"写法）。
   `model.OpRequest` 由前端序列化而来，**授权依据不许走它**。
4. **接缝位置**：`moveFileDetailed` 加一位 `checkLanding func(string) error`，在
   `claimDst` 拿到 `dst.path` **之后、`renameFile`/跨卷复制之前**调用一次。
   - 为什么在 claim 之后：这就是裁定句"按**实际落点**"的字面——递增改名（`name_1.ext`）之后
     的真实名字才知道；`dst.path` 的整条链解析能同时抓住"祖先目录被换成链接"和
     "落点名字本身被换成链接"两种形。
   - ★ 自觉代价（记为残余窗口，不另立号）：`claimDst` 的 O_EXCL 创建**早于**复审，
     所以目标树若已被换成外部链接，会在外部留下一个**零字节占位文件**；复审不过即
     `dst.release()` 按身份证明删掉它（这是本包已有的自有物删除规矩，不吞别人的文件）。
     数据一个字节都不出去：改名与复制都在复审之后。
   - `MkdirAll(targetDir)` 仍在更早处，本批不动它（它只建目录、不写内容；把它也纳入
     复审要在 claim 之前再问一次名义路径，属"两次问"的形状 ⇒ 见 §4-1）。
5. **拒绝即失败，不静默**：`ocFailed` + 错误文案点名"目标位置在本次操作期间已不在授权目录内"，
   源文件不动、结果集不移除（`settle` 既有语义）。文案是自撰中文 ⇒ 走 M202 的 B 档，
   不会再被套一层壳（两批在此咬合）。

## 3. 判据（红探针在前）

- **P-1 真改前红**（根包，只用**改前就有**的公开面）：`newTestApp` + `authorizeDir(root)` +
  结果集里一份 dup；目标 `root/sub` 合法建好；用既有接缝 `opsExecuteFn` 包住真实执行，
  在包装里把 `root/sub` **换成指向 outside 的符号链接**（模拟派发窗口内的第三方），
  再放行真执行，`waitOpsDone` 后断言三格：① 该项记 Failed 且文案点名授权；
  ② 源文件仍在原位；③ `outside` 里**没有**任何新文件。
  改前三格全红（rename 顺着链接写出去）⇒ 这是本批唯一的**真**改前红，不靠变异。
- **P-2 机制格**（`internal/ops`）：注入恒拒绝的 `MoveLandingAllowed` ⇒ 该项 Failed、
  源在、`targetDir` 里不残留占位空文件（`dst.release()` 生效）。
- **P-3 传参正确性**（`internal/ops`）：`beforeClaimRename` 钩子里记录钩子时刻 `targetDir`
  的实际形态，注入的 checker 断言自己收到的参数**就是** `dst.path`（含递增名那一形），
  且每个条目恰好被问一次。拦的是"复审被接到名义目录上"或"整批只问一次"。
- **P-4 默认不改变行为**（`internal/ops`）：`Options.MoveLandingAllowed == nil` 时
  move 腿与改前完全同形（正常成功）。★ 这是**护栏**不是红探针（AS-K2）。
- 变异预测（实现后跑；每条改前 `cp` 备份、还原 `cp`+`diff` 逐字一致）：
  - **Va** 执行器不传 checker（恒 nil）⇒ 预测杀 P-1、P-2；
  - **Vb** checker 调用挪到 `claimDst` **之前**、参数换成 `targetDir` ⇒ 预测杀 P-3；
  - **Vc** 拒绝分支不 `dst.release()` ⇒ 预测杀 P-2 的"不残留占位"那一格；
  - **Vd** `landingGuard` 改成"任一授权根与名义路径前缀匹配即放行"（回到 AS-H3 的 OR 形）
    ⇒ 预测杀 P-1（链接逃逸又被放行）。

### 3.1 实测对账（2026-09-24 本机 darwin/arm64 现跑；每条 `cp` 备份、还原 `cp`+`diff` 逐字一致）

| 变异 | 预测杀 | **实测杀** | 对账 |
|---|---|---|---|
| Va 执行器恒传 nil | P-1、P-2 | **P-1、P-2、P-3** | ★ **预测过窄**：P-3 断的是"checker 被问到、且问到的是实际落点"，接线一断它同样收不到参数 ⇒ 三格同倒。不改预测原文 |
| Vb 调用挪到 `claimDst` 之前、参数换成 `targetDir` | P-3 | P-3（两子格：参数是目录不是落点 + 第二个条目的落点不符） | 一致。★ 且 **P-1 在此不红**——名义目录自身已被换成链接，"问目录"这一形在这一格也问得出来；这一格正是"为什么必须问实际落点"的独立证据 |
| Vc 拒绝分支不 `dst.release()` | P-2 的"不残留占位"那一格 | **P-1、P-2** | ★ **预测过窄**：根包 P-1 的格③钉的就是"越权落点里不得多出任何东西"，占位不释放就在 `outside` 留一个零字节 `dup1.bin` ⇒ 两格同倒 |
| Vd `landingGuard` 回到 OR 形 | P-1 | P-1（ops 包三条不受影响） | 一致 |

四条各自至少杀掉一格、无一漏杀；两格差集同为"预测窄了"（判据面之间的耦合比设计段估计的强），
如实记在这里。★ **P-1 是真改前红**：在实施前的树上直接跑（未打任何变异）四格全红，
读数见 §6.35——它与变异是两件事，不混记。

## 4. 边界

1. 名义路径（`claimDst` 之前）不复审：外部零字节占位那一格按 §2-4 的代价记着。
   真要把 MkdirAll/claim 也纳入，需要"问两次"或把 claim 拆成解析+创建两步 ⇒ 另案。
2. 只修 move 一条腿。trash/delete/hardlink/symlink 不吃 `TargetDir`；回撤腿
   （`undo.go`）的落点是记录里的 `OrigPath`，那是 M152/M199 钉过的另一族格子，不外推。
3. 与 M184 的"同批判可省往返"本批**不做**：M184 在 `GetOpRecord` 的条目明细循环里，
   与执行器的 move 循环不是同一个函数、不是同一次 RPC，合并只会把两格欠账搅成一格。
4. 窗口没有被消除、只是**收窄到单条动作前**：checker 与 `renameFile` 之间仍有纳秒级 TOCTOU，
   登记行自己写了"窗口真被利用需并发第三方"。本批不假装做到了原子。
5. 授权集快照的副作用（操作中途用户新选的目录不被本批后续条目接受）是**裁定的直接后果**：
   放行与否的时刻必须唯一。表现为该项失败并点名，不静默。
