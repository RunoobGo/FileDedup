# 全仓审查修订批（第四轮）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 兑现 2026-09-22 七路只读全仓审查的新发现——2 条高危、约 12 条中危、一批低危与文档错位——并更新台账与门禁。

**Architecture:** 沿用本仓既定形状：ops 管线"身份复核紧贴动作"（AS-H4/OPS-7 同型）、fscase 家族"预热 + 锁外探测"（AS-R3/APP-1 同型）、门禁"正向断言 + 负控制"（AS-K 系同型）、前端"唯一判据/唯一文案方 + 接线锚"（M15/M79 同型）。不新增抽象层，不扩大改动面。

**Tech Stack:** Go 1.x（Wails v2 后端）、Vue3/TS（前端）、bash+python3（门禁脚本）、GitHub Actions。

**Spec:** 本轮审查证据基线 = HEAD `35b2d3c`（CI run 35742927520 三 job 全绿、工作树干净、门禁 rows=15 PASS=14 SKIP=1）。七路代理报告要点已收录于本计划各任务的"证据"段；**高危 2 条与中危 6 条已由主代理逐条开码核实**，其余低危条目坐标来自代理报告，实施时须按设计段纪律先开码重取坐标再动手。

## Global Constraints

- **禁提交/禁推送**：所有批次只改工作树；每批完成 + 回归全绿后向用户汇报，提交与推送另行授权（[[feedback-review-fix-workflow]] 既定约束，覆盖本计划所有任务里的"commit"语义）。
- **禁改断言求绿、禁扩大改动面**：沿用 docs/04 §6.8.0 硬约束。凡设计与实施偏差，就地记账不许静默。
- **Skip 不算通过**（AS-K2）；换夹具必须重做一次变异（§21 批口径 3）。
- **ID 纪律**：本计划用 R 编号（R<n>-<序>）作临时引用；正式 M 编号只在复核通过、划账写 docs/04 登记表时分配，不许预定区间。
- **每批回归底线**（[[regression-verification-standard]]）：`gofmt -l` 空、`go build ./...` + `go vet` ×3 平台、`go test -race -count=2 ./...`、根包 `-count=4`、`cd frontend && npm run build`、`bash scripts/run-gates.sh`（15 行读数如实记录，第 15 行本机 SKIP 不算通过）。
- **文档对齐口径**：行为变化 → 改 docs/09（+README 功能条目）；docs/01–03 冻结不动；docs/04 只增行不改写既有结论（登记表三约束）。
- **版本口径**：0.5.0、6 取值位/5 文件，任何触碰后跑 `scripts/check-version-sync.sh`。

## 待裁定清单（开工前需用户逐条拍板，未裁项不进批次）

| # | 事项 | 材料 | 建议 |
|---|---|---|---|
| J-1 | **CI 腿 smoke-symlink rc=2 是否改判红**（ci.yml:160-173 现为 warning+绿）。与既有 M8/M133 裁定"环境受限不怪代码"冲突，属推翻旧裁定，只能用户裁。ubuntu runner 有 sudo+tmpfs，rc=2 只可能是 runner 退化；同文件对 test-frontend-logic 的 rc=2 就判红（ci.yml:81-84），双重标准。 | 门禁路报告 M-3 | 改判红：唯一真跨卷防线在 CI 上消失不应只留 warning |
| J-2 | **发布腿加固方式**：build.yml release job 仅 `needs: build`、测试只有 `go test -count=1`（无 -race，:106）、`softprops/action-gh-release` 无 `draft: true` → CI 红的提交打 tag 即可一键公开发布。 | 门禁路报告 M-2 | 两者都做：release 加 `draft: true` + 发布腿补 -race；是否加跨工作流 CI-绿前置由用户定 |
| J-3 | **M105 嵌套卷归属**：卷语义按"根"传值（scanner.go:421/:481→verdictFor :749），根子树内嵌另一卷时排除放宽被错用（方向 fail-safe：少扫不错删）。与 M36 折叠键同构型（docs/04:1358/:1924 已登记折叠侧），放宽侧消费面未登记。 | diff 路发现 1 / 扫描路 M-D | 本批只登记（M36 同族残留），按条目问卷属生产面改动，另行裁定 |
| J-4 | **M47/OPS-2 是否提级**：trash_linux.go:83-102 跨卷复制后按路径盲删源，已登记未实施（"本机造不出第二挂载卷"）。CI Linux 腿可用 loop 设备造真第二挂载兑现红。 | 删除管线路"对台账的提醒" | 提级进本批 D 组（CI 腿造卷），或维持登记 |
| J-5 | **M75(b)**（历史遗留唯一待裁）：cache UPSERT `full=excluded.full` 无条件覆盖。 | docs/04 §6.14 表尾 | 维持原裁定材料，本轮不新增证据 |
| J-6 | **低危杂项处置**：F 组 16 条（见 Task F1 清单）——"登记不修"一把抓，还是把一行级小修（注释漂移、lastEvs 重置等）随批兑现？ | 各路报告 | 一行级且无行为争议的 6 条随批修，其余登记 |

---

## 审查结论总览（本计划的依据）

**总体评价**（七路一致）：判据链、删除管线、门禁工程的密度与台账纪律属同类项目第一梯队；本轮未发现任何"能造出假重复组"的新路径（错删方向的主防线完好）。剩余风险集中在三类：
1. **"复核与动作不紧贴"的一致性漏网**——delete 腿（高危 R1-1）与 undo 同卷腿（R2-1）是 AS-H4/OPS-7 同型仅存的两处；
2. **"无声失效"**——fscase 探测挂死无取消（R2-2/R2-3）、坏 glob 静默不命中（R2-4）、门禁对账粒度粗于数据面（高危 R1-2）、前端三处漏网入口对用户说假话（R3-1）；
3. **发布面弱于开发面**——release 非草稿、测试面弱于 CI（J-2）。

**已核实为无问题**（不立任务）：哈希漏斗无截断路径、缓存失效链无漏项、pathnorm 无双实现、scanner 并发/取消收口干净、锁纪律与账本耐久性达标（history synchronous=FULL、beginJournal fail-closed）、CLI/GUI 无判据漂移、benchgen 确定性成立、quarantine 锚定正确、version-sync 完备、ERROR_NOT_SAME_DEVICE=17 等 Windows 常量全部与 x/sys 权威值一致、近两批裁定实施面与设计判据逐条一致（无断言改软、无 Skip 放宽）。

---

## A 批：高危（先行，独立可交付）

### Task A1: delete 腿补"紧贴动作"的身份复核（R1-1，高危，已开码核实）

**Files:**
- Modify: `internal/ops/executor.go:419-441`（guardContent 交出 vid）与 `:649-663`（delete 分支）
- Test: `internal/ops/executor_delete_recheck_test.go`（新建）

**Interfaces:**
- Consumes: `VerifyFile(e, groupHash, pool) (Verdict, fsid.ID)`（verify.go:52，vid 现在 `v, _ :=` 被丢弃）；`identityCheck(path, id) (identityVerdict, string)`（verify.go:133）；包级缝 `verifyFileFn`（executor.go:158）、`fsidFromPathFn`（verify.go:103）。
- Produces: `guardContent(i, e) bool` 改签名为 `guardContent(i, e) (handled bool, vid fsid.ID)`（或等价形状）；delete/hardlink/symlink 三腿中**只有 delete 腿**消费 vid 做动作前复核（另两腿 Merge 内部已紧贴改名有 `identityGuardSentence`，move.go:125 / symlink.go:84，不动）。

**证据（已核实）**：guardContent 的全量 BLAKE3 对大文件耗时秒到分钟级，哈希绑 open 那一刻的 fd；重算期间第三方 rename 顶替路径时哈希照样 VerdictPass，随后按路径 `os.Remove` 永久删除顶替者、账本记 OK。delete 无回收站无 undo，是全管线唯一不可逆腿。docs/09:463 只承诺"身份复核之后再读内容"，未承诺内容复核之后仍验身份——防线覆盖面与其余四腿不对称。

- [ ] **Step 1: 写 RED 测试（行为级：哈希窗口内顶替，delete 腿必须拦下且不碰顶替者）**

```go
// executor_delete_recheck_test.go
// R1-1：guardContent 的长哈希与 os.Remove 之间被 rename 顶替 → 必须 Failed，
// 顶替者必须原样在盘上。缝：verifyFileFn（executor.go:158）在返回 VerdictPass
// 之前完成顶替（模拟"哈希绑旧 fd 通过、路径已指向新文件"）。
func TestDeleteLegRechecksIdentityAfterContentRecheck(t *testing.T) {
    // 夹具：dir/dup.bin（内容=组哈希对应内容）、dir/foreign.bin（顶替者，内容不同）
    // 钩子：verifyFileFn = 原实现算完 → os.Rename(foreign, dup) → 返回 (VerdictPass, 旧文件的 resolved ID)
    //   旧 ID 可用 fsid.FromFileInfo(顶替前 Lstat(dup))——本测试只在能解析 ID 的卷上跑，
    //   前提自检（fsid 是否 Resolved）放在 Execute 之前，不 Resolved 即 t.Skipf 并打印原因（M91 教训：自检必须在动作前）。
    // 断言：Execute(delete) 后 —— Failed 恰 1 条且 err 含"被替换"或"无法确认"；
    //   dup.bin 仍在盘上且内容 == foreign 原内容（顶替者没被删）；OK/Reclaimed 归零；账本无 done。
}
```

- [ ] **Step 2: 跑测试确认红**

Run: `go test -race -count=1 -run TestDeleteLegRechecksIdentityAfterContentRecheck ./internal/ops`
Expected: FAIL——现状是 `os.Remove` 删掉顶替者、条目记 OK（`OK=1` 而非 `Failed=1`）。红的面貌必须与预测一致，不一致先停下重读码。

- [ ] **Step 3: 最小实现**

executor.go guardContent：`v, vid := verifyFileFn(...)`，VerdictPass 时把 vid 交回调用方。delete 分支：

```go
case "delete":
    runIndexed(ctx, len(toProcess), opWorkers, func(i int) {
        e := toProcess[i]
        if guardIdentity(i, e.Path) { return }
        handled, vid := guardContent(i, e)   // R1-1：vid = 内容复核通过那一刻的身份
        if handled { return }
        if vid.Resolved {                     // 未解析 ID（FAT/exFAT）由既有放行分支接住，与 identityStill 同口径
            if v, why := identityCheck(e.Path, vid); v != vSame {
                settle(i, outcome{code: ocFailed, stage: "verify",
                    err: fmt.Sprintf("内容复核通过后、删除前文件被替换（%s），已拦截", why)})
                return
            }
        }
        if err := os.Remove(e.Path); err != nil { /* 原样 */ }
        settle(i, outcome{code: ocOK})
    }, onPanic)
```

hardlink/symlink 两腿的 guardContent 调用点同步改双返回值但**不消费 vid**（各自 Merge 内已有紧贴守卫），以 `_` 接住并注释指向 move.go:125/symlink.go:84。

- [ ] **Step 4: 跑测试确认绿 + 变异验证**

Run: `go test -race -count=1 -run 'TestDeleteLeg|TestM91' ./internal/ops`
Expected: PASS。变异：临时把 `vid.Resolved` 判断改成 `false` 短路 → 新测试必须红（证明断言真的接在判据上）；还原后 `go test -race -count=2 ./internal/ops` 全绿。

- [ ] **Step 5: 全量 ops 回归 + 文档**

Run: `go test -race -count=2 ./internal/ops` 与 `GOOS=windows go vet ./internal/ops`、`GOOS=linux go vet ./internal/ops`。
docs/09 §6.4（:463 附近）补一句：delete 腿在内容复核之后、删除之前还有一道身份复核；措辞与三格文案纪律一致（不把"无从判定"说成"被修改"）。

- [ ] **Step 6: 汇报（不 commit，等授权）**

---

### Task A2: smoke-cli 门禁从"计数对账"升级为"逐组对账 + failed 硬断言 + shapes 消费"（R1-2 高危 + R4-1 中危，已开码核实）

**Files:**
- Modify: `scripts/smoke-cli.sh:70-140`（对账段）
- Reference: `cmd/benchgen/main.go:35-52`（manifest 的 `groups[].Size/Files` 与 `shapes` 字段，现有数据未被消费）
- Modify: `docs/04-开发与测试计划.md` §3.4（"逐项对账"措辞与实现对齐——实现补齐后该句改真）

**证据（已核实）**：现对账只有 `want_groups`（组数）与 `want_total`（文件总数）两项；`failed` 只进三跑互比键（:80）从未断言为 0；`reclaimable/dup_files` 未与 manifest 推得值比对；benchgen 注释承诺"门禁据 shapes 区分没覆盖"但 smoke-cli 全文不读 shapes。失败模式：某组成员被一致性剔除（短读/预筛收敛过头、组仍 ≥2 成员）→ 组数与 files_total 不变、三跑照样一致 → 绿。这正是脚本头 :126 自己写明"三跑互比发现不了三跑一起漏"却没堵上的那一格。

- [ ] **Step 1: 先在脚本里写出期望值推导（python 段内），单独打印**

```python
# manifest 逐组期望：每组 reclaimable = size*(n-1)，dup_files = n-1
exp_groups = sorted((int(g["size"]) * (len(g["files"]) - 1),
                     "\n".join(sorted(g["files"]))) for g in mani["groups"] if len(g["files"]) >= 2)
exp_reclaimable = sum(c for c, _ in exp_groups)
exp_dup = sum(len(g["files"]) - 1 for g in mani["groups"] if len(g["files"]) >= 2)
```

（字段名以 `cmd/benchgen/main.go:49-52` 实际 JSON 键为准，实施时开码重取。）

- [ ] **Step 2: RED（负控制先行）**——临时把某次 run 报告里一组的一条成员路径从比对键里剔掉模拟"收敛过头"不可行，改用直接负控制：本地临时把 benchgen 语料某组删掉一个文件重生成 → 新断言必须红在 `reclaimable` 或 `dup_files`，而旧断言（组数/总数）在该形态下仍绿（把这一读数记进划账，证明新断言接住了旧断言接不住的那一格）。

- [ ] **Step 3: 加三条硬断言**

```python
if base["failed"] != 0:
    print(f"\nFAIL: failed={base['failed']}，门禁语料不允许有失败项", file=sys.stderr); sys.exit(1)
if base["reclaimable"] != exp_reclaimable:
    print(f"\nFAIL: reclaimable={base['reclaimable']}，manifest 期望 {exp_reclaimable}（组成员被一致性剔除在此暴露）", file=sys.stderr); sys.exit(1)
if base["dup_files"] != exp_dup:
    print(f"\nFAIL: dup_files={base['dup_files']}，manifest 期望 {exp_dup}", file=sys.stderr); sys.exit(1)
```

（`base` 的键名以脚本 :70-83 现有 cmp dict 为准；`reclaimable/dup_files/files_total` 已在互比键里，取 base 即可。）

- [ ] **Step 4: shapes 消费（R4-1）**

```python
shapes = mani.get("shapes", {})
if not shapes.get("symlink", False):
    print("\nFAIL: manifest.shapes.symlink=false——语料形态退化，冒烟覆盖面缩水", file=sys.stderr); sys.exit(1)
if not shapes.get("case_pair", False):
    case = "FAIL" if sys.platform == "linux" else "WARN"   # darwin 默认卷不敏感造不出 case_pair，须打印不许静默
    print(f"\n{case}: shapes.case_pair=false（平台 {sys.platform}）", file=sys.stderr)
    if case == "FAIL": sys.exit(1)
```

（benchgen 是否已输出 `shapes` 键、键名何值，实施时开码核实 `cmd/benchgen/main.go:35-37` 与 manifest 实例；若未输出则先补 benchgen 输出面——那属于本任务改动面内。）

- [ ] **Step 5: 三跑真读数 + 全门禁**

Run: `bash scripts/smoke-cli.sh`（预期 rc=0，打印新增的逐组对账读数）；再按 Step 2 负控制跑一次必须红、还原（用 `cp` 备份 + `diff` 自证还原，不用 `git checkout --`）。
Run: `bash scripts/run-gates.sh`，记录 15 行读数。

- [ ] **Step 6: 汇报（不 commit，等授权）**

---

## B 批：后端中危

### Task B1: undo 同卷腿补身份留底复核（R2-1，中危，已开码核实）

**Files:**
- Modify: `internal/ops/undo.go:86-120`（undoSourceCheck）与 `:174`（restoreInPlace 同卷 rename 前）
- Test: `internal/ops/undo_identity_test.go`（新建或并入既有 undo 测试文件，实施时看现有文件组织）

**证据（已核实）**：undoSourceCheck 对 DestPath 做 Lstat→size→全量 BLAKE3（按路径 open），哈希通过后同卷分支直接 `renameFile(it.DestPath, target)`，中间无 (dev,ino) 留底复核；跨卷分支有完整 AS-H4 链（pathIdentity+copyVerifyFile+identityStill，undo.go:181-201）。哈希窗口内 DestPath 被顶替 → 改名回家的是顶替者、账本记回撤成功、回收站真数据去向不明。M4 只修了判据（size→哈希）没覆盖此窗口；M48/M56 只管 OrigPath 侧。

- [ ] **Step 1: RED**——夹具：账本条目 DestPath=trash/x.bin（内容与 it.Hash 一致）；钩子在 hashFile 期间把 x.bin rename 走、放入同内容的另一文件（inode 不同）→ 现状 rename 成功、回撤记 OK；断言应为 Failed 且错误含"被替换"，两个文件都还在盘上。前提自检（fsid Resolved）放 Execute 前，Skip 打印原因。
- [ ] **Step 2: 跑红**：`go test -race -count=1 -run TestUndoSameVolume ./internal/ops`
- [ ] **Step 3: 实现**——undoSourceCheck 开头 `srcID, err := pathIdentity(it.DestPath)`（连同 st 一起返回或经 UndoItem 传递，实施时选改动面最小的形状）；restoreInPlace 同卷分支 rename 前：

```go
if srcID.Resolved && !identityStill(it.DestPath, srcID) {
    return "", fmt.Errorf("%s中的文件在校验后被替换（inode 已变化），已拦截: %s", where, it.DestPath)
}
```

跨卷分支已有同型复核，不重复加。硬链回撤腿（undoHardlink）若共用 undoSourceCheck 则自然受益，实施时开码确认覆盖面并在注释写明"三腿同口径"。
- [ ] **Step 4: 绿 + 变异**（把 Resolved 短路 → 必须红；还原）+ `go test -race -count=2 ./internal/ops`。
- [ ] **Step 5: docs/09 回撤章节补一句判据（与 M4 那句同段）；汇报不 commit。**

### Task B2: fscase 探测的阻塞面收敛——扫描启动腿（R2-2，中危，已开码核实）

**Files:**
- Modify: `internal/fscase/fscase.go:204-231`（probe：OpenFile/Lstat/Remove 无 ctx 无超时）与调用链 `internal/scanner/scanner.go:233→641`（dedupeRoots 在 worker 启动前同步执行）
- Test: `internal/fscase/probe_ctx_test.go`（新建）

**证据（已核实）**：死挂载（NFS 硬挂载/拔走的 SMB）上任一 syscall 挂住 ⇒ 扫描永久停在 Scanning；`Pipeline.Cancel` 只 cancel ctx，探测不读 ctx。AS-R3 只修了 app 层持锁探测，扫描器这一腿同族未登记（grep docs/04"死挂载"仅命中 app.go 条目）。

**设计裁定点（实施前确认形状，二选一，倾向前者）**：
(a) probe 带超时：用 `os.OpenFile` 无法直接设超时，改为"探测前先看卷型"——`statfs` 判为远端型（NFS/SMB/AFP 等，media/probe_darwin.go:74 已有 statfs 用法）直接走 fromVolumeType/Default，不写探针；本地卷探测挂死属内核态不可中断，超时救不了，如实写明边界。
(b) Verdict 调用点接 ctx：改动面大（fscase 公开签名变更、全部调用方过 ctx），且同样救不了不可中断 syscall。
⇒ 采 (a)：把"远端卷不写探针"钉成判据，同时给 probe 挂一个可注入的超时钩子供测试证明"慢卷不拖死扫描启动"（钩子生产恒 nil，与 beforeActContentRecheck 同型）。

- [ ] **Step 1: RED**——注入钩子模拟探测 syscall 阻塞（测试内用 channel 卡住 fireProbeHook 之后的路径），断言 dedupeRoots/扫描启动在 ctx cancel 后能返回错误而不是挂死；现状红（挂死直到测试超时）。
- [ ] **Step 2: 跑红**（用 `-timeout 30s` 让红可读）。
- [ ] **Step 3: 实现 (a)**：fromVolumeType 前置——probe 开头先 statfs 判卷型，远端型直接 `fromVolumeType(dir)` 不落探针文件；本地型照旧。verdict 缓存键与三态语义不动。
- [ ] **Step 4: 绿 + 负控制**（把远端判定短路 → RED 必须回红）+ `go test -race -count=2 ./internal/fscase ./internal/scanner`。
- [ ] **Step 5: docs/09 §3.1 补一句"网络卷不做写探针探测，按卷型给结论"（与 E 批 F1 的卷型档补写合并执行，避免两次动同一节）；汇报不 commit。**

### Task B3: ExecuteOperation 入口预热 fscase（R2-3，中危，已开码核实）

**Files:**
- Modify: `app.go:1866-1884`（`ops.ApplyProcessPolicy(groups, op.ProcessDirs, keepIDs)` 在 opsRunning=true 之后、无预热）
- Test: 根包既有 app 测试文件（实施时开码选位）

**证据（已核实）**：ApplyProcessPolicy 经 keep.go:361→normalizeDirs→fscase.Sensitive 可向前端指定目录写探测文件；死挂载挂住 → opsRunning 永不复位、CancelOperation 解不了、此后一切操作被拒。AS-R3/APP-1 只修了 Preview 与 ApplyKeepPolicy 两处（锁外 WarmSensitivity 预热），执行侧是家族第三处漏网。现状靠前端 M77 先调 Preview 的顺序缓解——结构上无保证。

- [ ] **Step 1: RED**——钩住 fscase 探测使第一次探测阻塞（若 B2 已落钩子则复用），直接调 ExecuteOperation（不经 Preview）→ 断言 opsRunning 不卡死/CancelOperation 有效；现状红。
- [ ] **Step 2: 实现**——入口取锁前 `ops.WarmSensitivity(op.ProcessDirs)`（与 app.go:1375-1388/:1446-1464 两处既有形状逐字同型），锁内改用查表版。若 WarmSensitivity 签名不吃 slice 则按既有调用点形状传。
- [ ] **Step 3: 绿 + `go test -race -count=4 .`（根包 flake 纪律）；汇报不 commit。**

### Task B4: 排除模式坏 glob 可见化（R2-4，中低，已开码核实）

**Files:**
- Modify: `internal/filter/filter.go:119`（Compile 无校验）与 `:310`（matchFold `ok, _ :=` 吞 ErrBadPattern）
- Modify: 扫描警告面（实施时开码确认警告如何进 `scan:done`/Failed 面板，前端 ScanView.vue:197 是裸文本框）
- Test: `internal/filter/badpattern_test.go`（新建）

**证据（已核实）**：`path.Match("[", "abc")` 返回 ErrBadPattern 被吞 → 坏模式永不命中、无声失效。方向是多扫（不误删），但用户写的排除整条失效且无任何留痕，违背"计数说真话"纪律。grep docs/04 `ErrBadPattern` 零命中，未登记。

- [ ] **Step 1: RED**——`Compile([]string{"[", "a/[z-a]"})` 应回传坏模式清单（或 error）；现状无出口，红。
- [ ] **Step 2: 实现**——Compile 时对每条模式跑一次 `path.Match(pat, "probe")` 捕获 ErrBadPattern，坏模式进编译结果的 `Invalid []string`；matchFold 的 `_` 保留（校验已前移）但加注释说明唯一合法吞错点。扫描启动时 Invalid 非空 → 进既有警告通道（与"权限不足跳过"同面板），文案给"排除模式语法无效，已按未排除处理：<模式>"——**fail-open 方向不变**（多扫安全），只是不再无声。
- [ ] **Step 3: 前端**——警告面板若已消费该类事件则零改动；否则在 ScanView 警告列表加一条渲染 + 接线锚（实施时开码定，若需前端改动则并入 C 批回归）。
- [ ] **Step 4: 绿 + 变异（把校验短路 → 红）+ docs/09 排除语义节补一句；汇报不 commit。**

---

## C 批：前端

### Task C1: 三处"唯一判据/唯一文案方"漏网入口（R3-1，中危 ×3，已开码核实）

**Files:**
- Modify: `frontend/src/components/GroupCard.vue:61,68,75`（"勾选为待删除项"/"标记为删除"/"勾选后将被删除"）
- Modify: `frontend/src/views/ScanView.vue:143`（恢复按钮 `:disabled="store.opsRunning"` 绕过 histBusy）
- Modify: `frontend/src/views/RecordsView.vue:259`（空态"支持对回收站/移动/硬链接合并进行回撤"）
- Modify: `scripts/test-frontend-logic.sh`（接线锚扩三条）

**证据（已核实）**：① GroupCard 对勾选项承诺"删除"，而五种操作只有 delete 是删除、默认是回收站（M22 裁定"trash 不得说释放/删除"）、处理策略还会收窄范围——aria-label 是屏幕阅读器唯一来源；② FE-3 已把互斥判据收进 `store.histBusy`（scan.ts:151）并在 RecordsView 落锚，ScanView 历史横幅是同形漏网，openHistory 期间可再点 → 双回包交错 bumpResultGen；③ 后端 undoableFor（app.go:1678）判 Windows trash 不可应用内回撤且 symlink 有 undoSymlink，空态总述做了平台无关假承诺又漏了软链——M79"判据归后端、文案归前端"机制被这句绕过。

- [ ] **Step 1: RED（接线锚先红）**——test-frontend-logic.sh 加三条锚：GroupCard 勾选项文案必须出自 opdisplay.ts 的"待清理"措辞（锚 `待清理` 且**不得**含 `勾选后将被删除`）；ScanView 恢复按钮必须锚 `histBusy`；RecordsView 空态必须锚"以各记录徽标为准"。跑 `bash scripts/test-frontend-logic.sh` → 三条红。
- [ ] **Step 2: 实现**——GroupCard 三处措辞改"勾选为待清理项/标记为待清理/冗余项（勾选后按所选操作处理）"并收进 opdisplay.ts 单一出口；ScanView `:disabled="store.histBusy"`；RecordsView 空态改"多数操作支持回撤（以各记录徽标为准）"。
- [ ] **Step 3: 绿**——`bash scripts/test-frontend-logic.sh`（预期 55+3=58 node 例或锚点计数相应增加，如实记录新读数）+ `cd frontend && npm run build`。
- [ ] **Step 4: 汇报不 commit。**

### Task C2: 高危判据测试缺口补齐（R3-2，中危·测试面）

**Files:**
- Test: `frontend/src/utils/pathpolicy.test.ts`（新建或并入既有 .test.ts，实施时看组织）
- Test: `frontend/src/stores/scan.test.ts` 增例

**证据**：`isGroupCrossVolume`（pathpolicy.ts:20）是前端唯一保留的路径判据（M7 刚修过假阳性方向——判错即引导用户进必然失败的提权 symlink 流程），55 例中零覆盖；`refreshProcCounts` 的 procReqSeq 陈旧回包守卫（scan.ts:601-622，直接决定确认框数字真假）无用例，而同文件 resultGen/histGen 都有 race 钉——同一缺陷族只测了两条腿。

- [ ] **Step 1: isGroupCrossVolume 用例**——同卷/跨卷/Windows 盘符/UNC/尾分隔符/相对路径混入至少 6 例，断言方向以 M7 修后的判据为准（实施时开码读 pathpolicy.ts 现实现取真值，不许按记忆写期望）。
- [ ] **Step 2: procReqSeq 用例**——模拟两次 refreshProcCounts 交错回包（旧回包后到），断言旧回包不得覆盖新真值；形状照抄 scan-race 既有用例。
- [ ] **Step 3: 绿 + 全前端回归**——`bash scripts/test-frontend-logic.sh`、`npm run build`。汇报不 commit。

### Task C3: UI 小修三连（R3-3/4/5，低危，坐标来自代理报告，实施时先开码复核）

**Files:**
- Modify: `frontend/src/components/ConfirmDialog.vue:46-53,130`（procCountError 后永久"计算中"→ 呈现错误 + 重试按钮）
- Modify: `frontend/src/views/RecordsView.vue:122-125,138-143`（undoOne 被 guard 拒绝时标签卡"执行中…"→ undoItem 返回受理布尔）
- Modify: `frontend/src/views/SettingsView.vue:21`（全仓唯一残留原生 confirm() → 收进 useModal）

- [ ] **Step 1: 每条先写 node 用例（能测的）或接线锚（纯视图的），跑红。**
- [ ] **Step 2: 实现三处；`npm run build` + test-frontend-logic.sh 绿。**
- [ ] **Step 3: 汇报不 commit。**

---

## D 批：门禁与 CI（J-1/J-2/J-4 裁定后排程）

### Task D1: build.yml 发布腿加固（R4-2，中危；范围依 J-2 裁定）

**Files:** Modify `.github/workflows/build.yml:89,104-106,176-202`

- [ ] **Step 1:** `:89` `npm install` fallback 改 `npm ci` 硬失败（lockfile 缺失应红不应静默解析最新依赖）。
- [ ] **Step 2:** `:106` `go test -count=1` 补 `-race`（若 J-2 裁"依赖 CI 绿"则此处保持轻量并加 `workflow_run`/required check，二选一按裁定执行）。
- [ ] **Step 3:** release job 加 `draft: true`（若 J-2 裁通过）；加 `concurrency: group: release-${{ github.ref }}`（R4-6 双 tag 并发覆盖）。
- [ ] **Step 4:** 本任务改动无法本机验证 → 记"代码已改、CI 验证未兑现"，推送后取真读数（推送另行授权）。汇报。

### Task D2: 门禁脚本假绿残角（R4-3/4/5，低危）

**Files:** Modify `scripts/smoke-symlink-assert.sh:120-124`、`scripts/test-frontend-logic.sh:111-112`、`scripts/run-gates.sh:81-137`

- [ ] **Step 1:** smoke-symlink-assert 的 stat 桩 elif（尾斜杠识别挂载点）是死分支——被测脚本两处调用（smoke-symlink.sh:114-115）均无尾斜杠 ⇒ 桩造不出"设备号不同"，rc=0 成功路径零覆盖。改按路径后缀（如 `/mnt`）区分两卷；改后 D 组七条判据必须真被桩驱动走一遍（打印每步读数）。
- [ ] **Step 2:** test-frontend-logic 接线锚 `'head: true'` 过泛（ResultView 任一 warn 含该串即满足）→ 锚更长上下文串（取 M80 那行的独特片段）。
- [ ] **Step 3:** run-gates.sh 行序违反 embed 顺序约束（build/vet/test 在 frontend build 之前，fresh clone 无 frontend/dist 齐红、stale dist 跑旧嵌入产物）→ harness 开头先产 dist 或存在性检查（与 §3.3 及 ci.yml:10-12 的既定顺序对齐）。
- [ ] **Step 4:** `bash scripts/run-gates.sh` 全量真跑记 15 行读数；smoke-symlink-assert 必须 rc=0 且七条判据逐条有输出。汇报不 commit。

### Task D3: CI 腿 smoke-symlink rc=2 判红（R4-M3；**仅在 J-1 裁定通过后执行**）

**Files:** Modify `.github/workflows/ci.yml:160-173`

- [ ] **Step 1:** rc=2 分支改为：ubuntu runner 上先自证 loop/tmpfs 可用性；不可用 → `exit 1`（"唯一真跨卷防线消失必须有人看"），可用而脚本仍 rc=2 → 照旧 exit 1（M133 既有逻辑不动）。本机 harness 的 SKIP 语义一字不动。
- [ ] **Step 2:** 台账：此行推翻 M8/M133 的"不怪代码"定性，划账时 dated 括注原文、不改写。推送后取 CI 真读数。

### Task D4: M47/OPS-2 Linux 跨卷 trash 兑现（**仅在 J-4 裁定提级后执行**）

**Files:** Modify `internal/ops/trash_linux.go:83-102`（复制后按路径盲删源 → pathIdentity+identityStill 同 AS-H4 链）；`.github/workflows/ci.yml` linux 腿加 loop 设备造第二挂载的真跑用例。

- [ ] **Step 1:** CI 造卷脚本（`dd`+`mkfs.ext4`+`mount` 到 tmp 挂载点，sudo 可用）跑既有跨卷 trash 用例——现状应红（盲删源窗口无守卫，用例断言"顶替者不被删"）。
- [ ] **Step 2:** 修 trash_linux 与 AS-H4 同型；CI 绿 + 本机 darwin 全量回归不受影响（linux-tagged 用例本机只 vet）。

---

## E 批：文档一致性（可与 B/C 并行）

### Task E1: docs/09 / README / 附录 A / specs 六处错位（R5-F1~F6）

**Files:** Modify `docs/09-用户手册.md:102,277,307`、`README.md:16`、`docs/04-开发与测试计划.md:5100（附录 A）与 :93/:98（§3.1 内联计数）`、`docs/superpowers/specs/2026-09-20-scenario-optimization-design.md:4`、`docs/superpowers/specs/2026-09-21-m6-implementation-design.md:99,146`

- [ ] **Step 1（F1，中）:** 09:102 补 M85 卷型档——"探针不可用时先问卷型：FAT/exFAT/NTFS/msdos 确证不敏感（排除随之放宽），卷型也拿不到才退平台默认"；09:277 "探测不到"改"探针与卷型都拿不到而退默认时不放宽"。与 B2 的 09 补句合并成一次改动。
- [ ] **Step 2（F2）:** 09:307 保留目录"分隔符不敏感"加平台限定（M63 后仅 Windows 成立；09:274 排除模式侧不动——filter.go:127 恒注入字面 `\`，三平台一致，手册正确）。
- [ ] **Step 3（F3）:** README:16 可回撤范围补"软链接合并"，功能列表补软链接合并条目（09:530 与 undo.go undoSymlink 均支持）。
- [ ] **Step 4（F4）:** 附录 A `LICENSE（MIT）与第三方声明文件` 拆两子项：LICENSE 勾上（根目录已在，MIT/RunoobGo），第三方声明文件留空。
- [ ] **Step 5（F5）:** scenario-optimization spec 状态行改"已实施（回执见 04 §6.x）"；m6 spec :99/:146 的 `/private` 整片写法加 dated 括注指向 M84 八子树（§27.5 有裁定记录，§2/§6 缺指针）；两份 spec 顶部补偏差说明（对齐 symlink spec 范本与 README:72 的承诺）。
- [ ] **Step 6（F6）:** 04 §3.1 表内 L1"516 Test/19 例探针"、L6"3 个 Benchmark"改指向 §3.2 现行真值（758/4），不改写既有追记。
- [ ] **Step 7:** `bash scripts/check-version-sync.sh`（09 首部被动过，跑一遍自证）；汇报不 commit。

### Task E2: 台账划账与登记（每批实施的收口步，最后统一执行）

- [ ] **Step 1:** docs/04 新增 §6.25（本轮实施划账）：R 编号 → 正式 M 编号分配（复核通过才配号）、每条的 RED 测试名/变异读数/未兑现清单；登记表只增行不改写；被推翻的旧定性（若 J-1 通过）加 dated 括注。
- [ ] **Step 2:** 设计稿 `docs/superpowers/specs/2026-09-21-m6-implementation-design.md` 追加 §30（本轮设计段）：七路审查方法、每条发现的开码坐标与主代理核实记录、"未验证"清单。
- [ ] **Step 3:** §3.2 计数核对（第十次）：src_test/Benchmark/平台三值/前端 node+接线，全部量真值回填；README「测试与回归」若引用计数同步。
- [ ] **Step 4:** 低危登记（J-6 裁定"登记不修"的那些）：逐条一行进登记表（位置→现象→本机可验证性），含本轮代理报告的低危全集——undoSymlink 抢占窗口（M152 同族）、darwin trash 空 DestPath 文案、M84 清单缺 /private/var/vm、M91 残余窗口注释、根侧 EvalSymlinks 静默、superseded 写历史行、lastEvs 未重置、scanInFlight 注释漂移、ops:progress 无节流、fdd-cli defer、GetOpRecord 网络卷、filter rel 分隔符 vs M63（FC-2 同族残留 + filter_test.go:327 反向钉）、fscase Verdict 缓存无失效钩、sysguard `\` 误命中、cache.go:35 注释过期、safeHref 协议相对、format.ts EiB、M82 设计钉偏差补记、E 组扫描面缺 build.yml。

---

## F 批：一行级小修（J-6 裁"随批修"的部分，预计 6 条）

### Task F1: 无行为争议小修打包

**Files:** `app.go:592-628`（StartScan 锁内重置 a.lastEvs）、`app.go:684-699`（superseded 判定前移，不落历史行）、`app.go:703-707`（scanInFlight 注释改真）、`internal/ops/cache.go:35`（Dev 注释与 fsid I7 对齐）、`cmd/fdd-cli/main.go:124`（defer Close 移到 os.Exit 前显式调用）、`app.go:2111-2116`（GetOpRecord 注释登记网络卷风险，不改行为）

- [ ] **Step 1:** 每条先确认有无可测面：lastEvs/superseded 各写一条根包用例（重扫后 GetScanProgress 不返回旧终值；被取代扫描不占 MaxScanHistory 格），跑红。
- [ ] **Step 2:** 实现六条；`go test -race -count=4 .` 绿。
- [ ] **Step 3:** 汇报不 commit。

---

## 执行顺序与回归节奏

1. **裁定先行**：J-1~J-6 拍板前，A/B/C/E 批可先行（不依赖裁定）；D 批等 J-1/J-2/J-4。
2. A1 → A2（高危优先，独立交付）→ B1 → B2 → B3 → B4 → C1 → C2 → C3 → E1 → F1 →（裁定项）D1~D4 → E2 收口。
3. 每任务局部回归（包级 -race + 变异负控制）；每批全量回归（Global Constraints 配方）；E2 划账时跑一次 `scripts/run-gates.sh` 记 15 行终读数。
4. 全程工作树改动，**每批汇报一次，提交/推送等用户授权**；推送后 CI 读数如实回填（复批纪律照 §6.21/§6.24 形状）。

## 风险与边界（不写成的话）

- A1 的 vid 在 FAT/exFAT 未解析时放行——与既有 identityStill 同口径，**不是新增漏洞但也不是全覆盖**，划账须写明。
- B2 只救"远端卷不写探针"，本地卷死挂载挂死仍不可中断（内核态），如实写边界。
- 本轮低危条目坐标多来自代理报告未逐条主核——实施每条前必须开码重取坐标（设计段纪律），坐标漂移就地记账。
- 计数面：C1/C2 会动前端 node 用例数（55→?），E2 的第十次核对应放在全部实施完成后，避免多次回填。
- 本计划不触碰：M75(b)、M152/M153/M154 本体、门禁第 15 行本机 SKIP 语义、docs/01–03 正文。
- **app.go 拆分**（2470 行，绑定层混入视图装配/平台命令/回撤编排；D 路设计建议）属大重构，与"不扩大改动面"纪律冲突，本批不做、不进登记表裁定面——是否立项由用户单独定。
