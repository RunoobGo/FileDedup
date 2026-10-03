# 设计段：2026-09-17 外部补丁「转做线索清单」逐条变异核验（M409 起自由号）

日期：2026-10-03　批次性质：**核验批**（零产品代码变更，除非核验读出 B 态）
对象：`/Users/just/Desktop/Desktop/{0001-fix-P0-P3.patch, FileDedup-review-fixes.diff, SHA256SUMS.txt}`
HEAD 起点：`91053be`（= `origin/main`）

---

## 0、范围与号

**本批不合并那份补丁**（合并可行性已在上一轮口头报告里否掉，读数见「一」）。用户裁定改走第二条路：
把补丁正文声称的 **20 条修正**当作**线索清单**，逐条对到当前代码上核验，
凡核验出「修正已在位、但没有任何测试能抓住它被改坏」——那一条**占号**，从 **M409** 起顺延。

★ 为什么「无守卫」算缺陷而不是算「已落地就行」：本仓有两次同族记账——
M93「恒绿却什么都没测」、M324 门禁自述「清单为空必须红」从未成立过。
一条修正若没有能抓它的判据，它就会在下一次重构里**静默漂回去**；
补丁本身正是活证据：它是 09-17 落的，两周里 `verify.go` 已被改写三轮，
「mtime 快路径」这条修正只剩注释在守。**注释不是判据。**

本批面上：不改产品代码、不改测试断言、不动 `.github/workflows/`（D1/D3 由用户把着）、
不动 tag、不派发。`git add` 只点名本批涉及的 `docs/` 文件。

---

## 一、现读取证（全部为本会话实测，复取命令随读数给出）

1. **清单不覆盖代码产物**。`SHA256SUMS.txt` 285 B，三条条目全是 0.4.0 时代的
   `.exe`（`FileDedup-0.4.0-win-x64-setup.exe` / `-user-setup.exe` / `FileDedup.exe`）；
   本机 `shasum -a 256` 出的 `ce0641ac…`（diff）与 `ffee3f8b…`（patch）在清单里查不到。
   ⇒ 这份 manifest 不构成对两份代码产物的来源认证。

2. **两份是同一补丁**。`sed -n '/^diff --git /,$p'` 截出的 `.patch` 正文 138,093 B 与
   `.diff` 138,081 B **只差 12 字节**，差的正是结尾 `-- \n2.34.1\n` 这行 git 签名。
   ⇒ 不是两轮独立审查，是一个产物的两种封装。

3. **出处与作者**：邮件头 `Date: Thu, 17 Sep 2026 07:17:11 +0000`、
   `From: RunoobGo`（本仓库所有者），正文自述依据 `docs/code-review-2026-09-16.md`（该文件不在树里）。

4. **同轮早已落库**：`09a4929`「fix: 落地 2026-09-17 代码审查修复（R1-R4、C1-C17）并补充回归测试」，
   同日 20:44 +0800，`--numstat` **57 个文件**，是补丁 42 个文件的超集
   （多出 `crossdevice_windows.go`、`executor_panic_test.go`、`hardlink_merge_test.go`、
   `cache_c5_test.go`、`cache_integration_test.go`、`trash_darwin*.go`）。
   补丁的提交号 `39ffe7db5ed5…` 本机 `git cat-file -t` 报 `could not get object info` ⇒ 未落库的那一版。

5. **两方合并打不上**：`git apply --check` → `rc=1`，42 个文件里 **25 个拒收**、`^error:` 92 行。

6. **三方合并也是回退**：`git apply -3 --check` → `rc=1`，**24 个冲突 / 10 个"干净"**；
   那 10 个干净的恰好是当前**已是目标态**的空转项（`.gitignore:2-3` 注释、`ci.yml:10` 注释、
   `/filededup` 忽略三行——三处文字与补丁 post-image 逐字相同）。

7. **强行合并会拆掉身份守卫**：补丁第 3469 行写的是
   `func VerifyFile(e *model.FileEntry, groupHash [32]byte, pool *hasher.Pool) Verdict`，
   当前 `internal/ops/verify.go:52` 是 `(Verdict, fsid.ID)`，在其上多了两层后续修复：
   **H2**（open→fstat→经 fd 哈希，返回校验那一刻的 ID 供 `identityStill` 在破坏性动作前复核）、
   **AS-H1**（2026-09-20 全仓审计：身份取自 `fsid.FromFile(f)` 句柄查询，否则 Windows 上
   `fromInfo` 恒返回未解析 ID ⇒ 五处破坏性前复核与 keepID 守卫**全部空转**）。

8. **五个测试文件会被截断**（补丁按 new-file 写，覆盖已演化版本）：
   `app_p0_p1_test.go` 379→193（−186）、`internal/ops/trash_linux_test.go` 334→153（−181）、
   `app_preview_p3_test.go` 291→145（−146）、`internal/ops/cancel_test.go` 217→125（−92）、
   `internal/ops/move_p2_test.go` 152→143（−9），合计 **−614 行**；
   另四个（`app_p3_linux_test` 96、`app_p3_test` 164、`cache_p02_test` 159、`pipeline_p3_test` 123）与 HEAD 同形。

9. **baseline 必须先在位才有变异意义**：
   `go test -count=1 ./internal/{filter,cache,dedup,ops,scanner,fsid,hasher}` →
   **全 ok，`rc_baseline=0`**（用时读数：filter 0.373s / cache 6.473s / dedup 3.157s /
   ops 1.474s / scanner 1.104s / fsid 0.294s / hasher 0.436s）。

---

## 二、判据（三态，逐条只能落进一格）

- **A 态｜已落地且守卫在位** = ①当前代码有可指认的修正本体（文件:行）②把它改坏后，
  至少一个既有测试变红。⇒ **不占号**，只在 §6.80 记一行读数。
- **B 态｜已落地但无守卫** = ①在位 ②改坏后相关测试**全绿**。⇒ **占号**（M409 起），
  缺陷表述统一写「修正 X 在位但无判据能抓它被改坏 ⇒ 会静默漂回」，修法在下一批（补测试，不改实现）。
- **C 态｜作废/被替换** = 该线索的判据已被后续裁定改写（例：剪枝策略被 M 系列重定义、
  或补丁自述已撤回的项）。⇒ 不占号，正文写明"被哪一条后续裁定替换"。
- **D 态｜本机无法核验** = 平台腿取不到（见「四」）。写作 **`未兑现`**，
  **绝不得写作"已通过"**（交付纪律原话）。

★ 判据的判据：变红的测试必须**因这条修正而红**，不是因编译失败或别的用例连带红。
每条变异后先看 `go build` 是否通过、红的是哪个 `Test*` 名字，读不进这条就不算 B/A，重做。

---

## 三、逐条变异设计（20 条线索 · 坐标为 91053be 现读）

回滚一律用 `cp` 副本（`cp <file> /tmp/fdd_rev/bak/<file>` → 改坏 → 跑 → `cp` 回），
**不使用** `git checkout --` / `git restore` / `git stash`（共享工作区禁用裸 stash）。
每格跑完立即 `git status --short` 取证，非空即视为回滚没做净、停批。

| # | 线索（补丁原话摘句） | 当前坐标 | 变异动作 | 跑哪儿 |
|---|---|---|---|---|
| L-1 | S1 去「mtime 未变即通过」快路径 | `internal/ops/verify.go:69`（size 判）`:74`（无条件 `HashFull`） | 把 `:74`~`:85` 换成 size 一致即 `VerdictPass`（退回元数据快路径） | `./internal/ops/` |
| L-2 | 二次扫描永久被拒（终态复位） | `app_scan.go:37`（放行 `StatusDone`）、`internal/model/model.go:83` | `app_scan.go:37` 改成只允许 `StatusIdle` | 根包 `-run 'Scan|Rescan'` |
| L-3a | 缓存命中也带采样进分桶 | `internal/dedup/pipeline_stages.go:367-392`（P0-2 注释 + `sampleGroups`） | 让带全量的命中文件绕过分桶 | `./internal/dedup/` |
| L-3b | 阶段 3 原位回填 Full 而非 append | `pipeline_stages.go:187` 候选平行槽位 + `:300`「四点采样全一致→信任缓存 full」 | 把信任分支改成一律弃用 | `./internal/dedup/` |
| L-3c | `AlgoVersion` 失效旧脏数据 | `internal/cache/cache.go:84`（`blake3-256+xxh64-4pt+id-v4`）`:271 enforceAlgoVersion` | 版本不符时不清表（去掉清空动作） | `./internal/cache/` |
| L-3d | Lookup 拒绝零采样行 | `cache.go:398`（Lookup SELECT 一带） | 删掉零采样守卫 | `./internal/cache/` |
| L-3e | UPSERT 保护 partial | `cache.go:490-501`（`CASE WHEN excluded.partial = 0x00… THEN 保旧`） | 改为无条件覆盖 partial | `./internal/cache/` |
| L-4 | `opsRunning` 快照与置位同临界区 + `StartScan` 反查 | `app_ops.go:38-44`（P1-1 注释）`:143` 置位、`app_scan.go:52` 反查 | 去掉 `app_scan.go:52` 那道反查 | 根包 `-run 'Ops|Scan'` |
| L-5 | ResultView 排序/扩展名 v-model 绑回 store | `frontend/src/views/ResultView.vue` + `stores/scan.ts` | 绑回局部 ref | `frontend`（vitest） |
| L-6 | `MaxThreads=64` 钳制 | `app_settings.go:86-91` | 去掉上钳 | 根包 `-run 'Settings|Thread'` |
| L-7 | 清理可取消（`Options.Ctx`+`CancelOperation`+`Cancelled`） | `internal/ops/executor.go`（ctx 臂）、`app_ops.go:212` 出口 | 执行循环忽略 ctx | `./internal/ops/` |
| L-8 | 剪枝加 `prunable()` 门控 | `internal/filter/filter.go:312`、`internal/scanner/scanner.go:406` | 去掉门控（一律可剪枝） | `./internal/filter/` `./internal/scanner/` |
| L-9 | 跨卷移动还原权限位与 mtime | `internal/ops/move.go:334-341 restoreMeta` | 删掉 `restoreMeta` 调用 | `./internal/ops/` |
| L-10 | `isCrossDevice` 只认真实 EXDEV | `move.go:458-469` | 退回「所有 LinkError 都算跨卷」 | `./internal/ops/` |
| L-11 | trashinfo 先写再移、失败回滚 | `internal/ops/trash_xdg.go:47-56` | 调换顺序 | `./internal/ops/`（★ darwin 上该文件参与编译，用例是否跑得到待读） |
| L-12 | Pause/Resume/Cancel 无任务时返回错误 | `app_scan.go:201-207` → `pipe.Pause()` | 改成静默 `nil` | `./internal/dedup/` 根包 |
| L-13 | `goTask` 守卫 panic | `app_lifecycle.go:246`、调用点 `app_scan.go:83` `app_ops.go:189` | 去掉 recover | 根包 `-run 'Panic|Task|Reset'` |
| L-14 | taskID/opID 加原子序号 | `app.go:292 taskSeq`、`app_scan.go:82`、`app_ops.go:148` | 退回纯时间戳 | 根包 `-run 'ID|Task'` |
| L-15 | main.go println → stderr + 非零退出 | `main.go:61`（P3 注释）`:76 os.Exit(1)` | 改回 `println` 且退出码 0 | 根包 `-run Startup` |
| L-16 | Linux RevealInFolder 优先 `--select`、全缺报错 | `app.go:717-732`（按 DE 探测） | 去掉探测/去掉全缺报错 | 根包（★ Linux 腿，本机 darwin ⇒ 预期 D 态） |
| L-17 | App 窄接缝 `emit` 替换 EventsEmit | `app_lifecycle.go:37` 等调用点 | 直连 `runtime.EventsEmit` | 根包（编译面 + `-run 'Emit|Error'`） |
| L-18 | `wails.ts` 补 `CancelOperation`/`Cancelled` | `frontend/src/wails.ts:452` `:554` | 删接口成员 | `frontend`（typecheck） |
| L-19 | 畸形顶层 `"docs` 目录改回 `docs/` + 01 三处契约 | 顶层无 `"docs`；`docs/01` 是否写 `CancelOperation`/`ops:error`/控制报错语义 | — | **只读核验**（无变异） |
| L-20 | 仓库卫生：README BLAKE3/Go 版本/embed、`.gitignore`、`ci.yml` 注释 | `.gitignore:2-3`+`:8-10`、`ci.yml:10`、README | — | **只读核验**（已逐字比对在位） |

**★ 本批没有"红探针可取"这一说，只有变异探针。** 理由：A/B 态的分界是「既有判据抓不抓得住」，
不是「缺陷当前红不红」——当前全绿（读数 9）。所以「修前必红」这条家法在本批**换形不换神**：
改坏必须红 = 该修正有守卫；改坏仍绿 = B 态成立。

---

## 四、边界（做不到就如实记 D 态，不写"通过"）

1. **本机是 darwin**。`//go:build linux` / `//go:build windows` 的兄弟文件与用例
   **不参与编译**（先例：`internal/fsid/fsid_windows_test.go` 只有交叉腿跑得到）。
   ⇒ L-16（Linux Reveal）与任何 Linux-only 断言只能做到「交叉 `GOOS=linux go vet` 干净」，
   记 **`未兑现`**；Windows 真机类（回收站、句柄身份）本机无宿主，同记 `未兑现`。
2. **前端腿要 npm**。L-5/L-18 落在 `frontend/`（vitest + typecheck），与本仓 Go 门禁是两条通道；
   若本轮不跑前端 ⇒ 同记 `未兑现`，不写"已通过"。
3. **不跑全量 16 行门禁做实现验证**（本批零产品代码），但**要跑第 16 行 `anchor-hit-rate`**：
   本批往 `docs/04` 与 specs 落笔会新增/改动 `file.go:NNN` 形态的文字 ⇒ 命中率读数会变。
   ★ 上一批踩过的坑（§6.79 记过）：账本正文里写 `scripts/run-gates.sh:297-302` 这类
   路径:行号，会被 `anchor-hits.awk` 当真锚点解析。本批正文一律用**反引号包整串 + 不写行号尾巴**
   的形态复取，落笔前后各取一次 `anchors=` 读数。
4. **不许为了让门禁变绿而改既有测试断言**——本批根本不改断言。
5. 变异期间若某个包本身红（baseline 之后被别的批影响）⇒ 立刻停批回滚，不带红做变异。
6. 占号只在 B 态成立后落；**拟号不等于分配，分配在复核之后**（本批第十次应用）。

---

## 五、落点

| 笔次 | 内容 | 文件 |
|---|---|---|
| ① 设计段（本文件） | 范围与号 · 9 条现读 · 三态判据 · 20 格变异设计 · 6 条边界 | `docs/superpowers/specs/2026-10-03-review-leads-m409-design.md` |
| ② 实施 | 逐格变异读数（红在哪个 `Test*`／绿 ⇒ B 态），含 baseline 与回滚取证 | 无产品代码变更；读数进 §6.80 正文 |
| ③ 划账 | §6.80 落册：20 条 A/B/C/D 判定表 + 号表（B 态才占号）+ 未兑现格 | `docs/04-开发与测试计划.md` |

复取命令（本设计段全部读数）：

```
git -C /Volumes/fx/Object/FileDedup rev-parse --short HEAD
git -C /Volumes/fx/Object/FileDedup apply --check -v <patch> ; echo rc=$?
git -C /Volumes/fx/Object/FileDedup apply -3 --check <patch> ; echo rc=$?
git -C /Volumes/fx/Object/FileDedup show --numstat --format="" 09a4929 | wc -l
sed -n '/^diff --git /,$p' <patch> | wc -c ; wc -c <diff>
shasum -a 256 <diff> <patch> ; cat <manifest>
go test -count=1 ./internal/{filter,cache,dedup,ops,scanner,fsid,hasher}
```
