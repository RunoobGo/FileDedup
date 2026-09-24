# 2026-09-24 · fscase 迟到探测 × TempDir 清理同窗 flake 的细化设计（M208/TST-12）

> 任务源：docs/10 手册批（纯文档提交 `390624f`）CI macos 腿红在
> `TestVerdictCtxHonorsDeadline`——正是 §6.25 本节五 / §6.28 本节五-4 挂账那条红的
> **第三次读数（第二次 CI 读数）、第一次留下完整错误签名并本机施压复现**。用户指令"执行两项待裁定事项"+"每次任务完成后…
> 跟踪 CI 情况，修复问题"⇒ 本批按"根因已定 → 修复落地"处置，不按"复跑转绿"处置。
> 划账新增 ID **M208/TST-12** 与 §6.30；既有挂账行（M155 观察项、M186）一字不改写。

## 1. 取证

### 1.1 CI 读数（红原样）

- run `35938027690` / job `107439407308`（`go test (macos)`），步骤 `go test -race -count=2`：

```
--- FAIL: TestVerdictCtxHonorsDeadline (0.05s)
    testing.go:1617: TempDir RemoveAll cleanup: unlinkat .../T/TestVerdictCtxHonorsDeadline2386869373/001: directory not empty
```

- head 是**纯文档提交** `390624f`（docs/10 + README），ubuntu/windows 腿全绿 ⇒ 红与手册无因果，
  是既有 flake 被满载 runner 抽中。读数史三格：① M155 取证时**本机加压跑**同窗口红过一次
  （§6.26 本节三"同一次跑里还夹了一条"，三次复跑未再现、根因未定）；② CI run `35884647251`
  第一次 CI 读数（§6.25 本节五）；③ 本格，第二次 CI 读数——**第一次留下 `directory not empty`
  的完整错误行**。★ ①"只在加压跑里现身"与 §1.2 本机复现的形态（单进程不红、施压必红）互证。

### 1.2 本机复现（根因从推断升为实测）

| 口径 | 命令 | 读数 |
|---|---|---|
| 单进程 | `go test -race -count=200 -run 'TestVerdictCtxHonorsDeadline\|TestVerdictCtxAbortsBlockedProbe' ./internal/fscase` | **0 红**（400 次跑全过）|
| 6 路并发施压 | 同上 `-count=100` × 6 进程 | **26 红 / 1200**（Deadline 21 + Abort 5），错误行与 CI 逐字同签名 |
| 对照组 | 同施压下 `TestVerdictCtxServesCacheWithoutProbing`、`TestVerdictIsVerdictCtxOnBackground` | **0 红** |

- 单进程复现不了、施压必复现 ⇒ 与 CI 形态一致（macos runner 上各包并行、机器满载）。
- 对照组 0 红把暴露面**收口到恰好两条**用例：只有 `TestVerdictCtxAbortsBlockedProbe` 与
  `TestVerdictCtxHonorsDeadline` 会在用例收尾**松手（release）**一支已被放弃的探测；
  另两条没有"迟到探针"这个物种（ServesCache 命中缓存不探测；Background 腿同步等到探测返回）。
- app 层同族用例（`TestExecuteOperationBlockedProbeDoesNotWedgeApp`）核过**无此暴露**：
  它在 `release()` 之后同步等 `ExecuteOperation` 经 `errCh` 返回才结束 ⇒ 迟到腿不存在。

### 1.3 根因（一句话 + 两半机制）

**一句话**：`VerdictCtx` 在 `ctx.Done()` 那一格按设计**不等**探测收尾（R2-2 的全部意义），
于是被放弃的那支探测会在**用例结束之后**、在交给它的那个目录里补做一轮 I/O；
这一轮 I/O 与 `t.TempDir()` 的框架 `RemoveAll` 撞进同一个窗口。

- 迟到腿做什么：`probe`（fscase.go，无 ctx 感知）在目录里 `os.OpenFile`（O_CREATE|O_EXCL）
  建探测文件 → `verdictFrom` 两次 Lstat → 无条件 `os.Remove` 清掉自己建的。可写目录上
  首试即出结论 ⇒ 恰好一次 create + 一次 remove。
- 撞窗为什么红：`RemoveAll` 先 `readdir` 再逐项 unlink 最后 `rmdir`；迟到 create 落在
  readdir 之后、rmdir 之前 ⇒ rmdir 报 ENOTEMPTY → `directory not empty` → 框架计为用例失败
  （`testing.go` 的 TempDir 清理报错格）。文件随后照被探测自己删掉——**没有任何残留**，
  红的是删除时序，不是盘上留了东西。
- 为什么 §31 M155 的窗口叙事没覆盖到它：M155 钉的是"迟到读方读包级注入缝"（已修，走锁）；
  当时依据 LIFO cleanup 推断"release 先、TempDir 删后 ⇒ 迟到 OpenFile 必 ENOENT"。
  该推断对**仅在 cleanup 里 release** 的现场成立，对 `TestVerdictCtxHonorsDeadline` 不成立——
  它在**函数体末行**就 `release()` 了，目录要等函数返回后才被删 ⇒ 迟到腿面对的是**活目录**。
  Abort 用例同理（体内 release 后还要验"松手仍拿得到实测结论"）。
  ★ 这是一格真实的"注释里的时序前提只覆盖一半调用现场"，划账照实记。

## 2. 修法（仅测试侧，生产码一字不动）

`internal/fscase/probe_ctx_test.go` 新增助手 `probeTempDir(t)`，替换上述两条用例的 `t.TempDir()`：

- `os.MkdirTemp` 自建目录 + 自注册 `t.Cleanup`：对 `os.RemoveAll` 做**有界重试**
  （每 10ms 一次、上限 3s；到顶仍失败才 `t.Errorf`）。
- **为什么重试不是和稀泥**（这是本设计唯一需要论证判据的点）：
  `RemoveAll` **成功一次**即目录不复存在 ⇒ 迟到探测的 `OpenFile` 从此必 ENOENT、走
  "创建失败"出口返回，**不可能再有新条目**——"某次 RemoveAll 成功"本身就是"该窗口已过"的
  机器证明。它不是"把清理错误咽掉"：若迟到腿永远不做 I/O（钩子没被松手）或目录里有
  别的不明占用，重试到顶照样红。
- 断言面**一字不松**：取消/超时返回 ctx 错误、零值结论、不进缓存（M126 格）、
  松手后仍拿得到实测结论——四格全部原样保留，改的只是"目录归谁删、怎么删"。
- **不选**的两条路（写下来防复议）：
  ① 给 `probe` 传 ctx 在 create 前查取消——新增生产接缝，违背 R2-2 批"夹具不新增接缝"
  的既有纪律，且只是**缩窗**不是关窗（check 与 create 之间照样可撞）；
  ② 体内不 release、改在 cleanup 里先删目录再 release——Abort 用例的"松手后同目录仍要
  拿到实测结论"断言就构造不出来（目录得重建，比现方案更绕且仍留同窗形状）。

## 3. 判据（修前必红 / 修后必绿 / 变异必红）

1. **修前必红**：§1.2 施压口径（6 路 × `-race -count=100`）在 HEAD 上 6/6 进程含红、
   合计 ≥20 条同签名 FAIL。已在设计段落盘前实跑取得。
2. **修后必绿**：同一施压命令（提到 `-count=200`×6 加大覆盖面）零红；
   单进程 `go test -race -count=2 ./internal/fscase` 全绿；`gofmt` 零文件。
3. **变异必红**：把 `probeTempDir` 的清理改回"单次 RemoveAll 不重试" ⇒ 施压口径必须
   重新出现同签名红（证明 2 的绿归因于本修形，而非负载抖动碰巧避开窗口）。
4. **全套门禁**：`scripts/run-gates.sh` 15 行读数照旧逐行进划账；这发红**本机单进程口径从不
   现身**（§1.2 对照），CI 的 macos 腿（同 `-race -count=2` 但机器满载并行）才是抓到它的那一口
   ⇒ 划账如实记"复现依赖满载施压，本机门禁单跑不拦"，不为它改 harness 判据（那是另一格的口子）。

## 4. 边界与不做什么

- **生产语义一字不动**：`VerdictCtx` 的"不等待、不进缓存"是 R2-2 的设计本身，本批不碰；
  迟到探测在生产目录里做一次 create+remove 的窗口**继续存在**，它无害的依据是：探测文件
  恒被探测自己无条件清掉、且 `worktemp` 的忽略规则（前缀+全数字）本就覆盖它——这两条
  分别由 `probe` 的 remove 腿与 M1/M13 的既有断言在册。
- 不收紧任何既有断言、不为绿改判挂账行：§6.28"不随本格转绿改判"针对的是**偶然转绿**；
  本批是"根因定位 + 修形落地 + 变异回红"三件齐，M208 行按已修记，并把三次读数链
  （§6.25 本节五 → §6.28 本节五-4 → 本批）写全。
- M186（`TestSupersededScanWritesNoHistoryRow`）是**另一条**挂账红，本批不碰、不改判。
- 本机 CI 腿（macos runner）的红只能以"CI 下次跑该用例绿 + 本机施压绿 + 变异红"三件
  代理；windows 腿的 TempDir 行为差异不引入新的断言（本修形平台无关）。

## 5. 回归测试设计

- 直接复用两条既有 ctx 用例本体（它们就是这一族的判据），**不新增断言语义**；
  新增的 `probeTempDir` 自身由 §3-3 的变异兜底：谁把重试删掉，施压口径立刻红。
- 给助手写页内注释（M150 口径，指符号不钉行号）：为什么不能换回 `t.TempDir()`——
  防止下一批"顺手统一夹具"把修形抹掉。
