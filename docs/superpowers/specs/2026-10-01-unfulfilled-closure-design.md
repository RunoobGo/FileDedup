# 2026-10-01 · 未兑现面收口批设计段（M405 / M402 / M401 / D3 判红臂 / M120＋D1 / A2 / A3 / `docs/05` 逐格）

用户指令原文：「逐一完成未兑现任务：M405（harness 行 6 只外显计数、名单随 mktemp 回收）、M401/M402、M120、
D3 的判红臂（ubuntu 腿恒 rc=0，反事实）、docs/05 其余真机格与 A2/A3。」

⇒ 这一批的**输入不是新发现**，而是 §6.71／§6.72／§6.73 三批各自收口时**自己写下的未兑现清单**。
七项全部点名，一项不落；逐项给落点、判据、改前红与兑现边界。

---

## 0. 号位与范围

### 0.1 本批不预拟新号

用户点名的七项里，**六项是"兑现一件已登记的事"**（M405、M401、M402、M120、A2、A3），一项是
"把一条只有反事实的防线变成本机可实跑的防线"（D3 判红臂）。按登记表约束 (1) 与 §6.73 五立过的读法：
**号表记缺陷，兑现动作不占号** ⇒ 本批不预拟任何号；实施过程中撞出来的**新**缺口才从 **M406** 起取号，
且"拟号不等于分配、分配在号表"（§6.26 纪律）。

### 0.2 M177／M334 这次做，但只做到"清单与下界一起抬"

登记行自己写着（M401 行原文）："给 E 组那套静态锚加一条……**同批把 M177/M334 的清单与下界一起抬**"。
M401 的新锚天然要扫 `.github/workflows/*.yml`（两份都扫，只扫 `ci.yml` 挡不住 `build.yml` 被改回裸
`go test`）⇒ **扫描面抬到含 `build.yml` 是 M401 的成立条件，不是顺路扩大范围**。
M177/M334 的**其余面**（把 D1 那三格假绿写法也纳入 E 组的 `$VAR` 全角锚）随之自然成立，
但**不宣称它们全部兑现**——它们的登记行只会被追记"扫描面已抬、下界已跟着走"，不改成"已实施"以外的判读。

### 0.3 本轮不做的

`build.yml` 里 D1 之外的加固、`smoke-cli` 的覆盖面、`## P0~P12` 里必须真机（GUI／注册表／第二物理卷／
AV／OneDrive）的那些格——**不是本批能凭现有环境做完的**，逐格判定与"缺哪一件硬件"写在划账 §6.74 一-B，
不假装销格。

---

## 1. M405 —— harness 行 6 只外显计数，名单随 `mktemp` 回收

### 1.1 现读基线（改前）

- `scripts/run-gates.sh:143` 行 6 = `go test -count=1 -v ./...`，日志写 `$LOGDIR/6-test.log`；
  `:146-155` 只 `grep -c` 打**八个计数**，**一条名字都不外显**。
- `scripts/run-gates.sh:34` `LOGDIR="$(mktemp -d)"`；`:49-57` `gates_cleanup()` 在 **FAILS=0 时 `rm -rf`**
  ⇒ 全绿那一跑，`6-test.log` 连同名单被回收。
- ⇒ 症状（§6.72 七·5 实录）：同一棵干净树、同一时间窗，行 6 报 `top_SKIP=7`，而
  `bash scripts/ci-go-test.sh -count=1 ./...` 三跑一律 `skip_top=6 roster_top=6`。**那 7 个名字永久拿不到**。
- ★ 这正是 M331 立的那类缺口的**本机版本**：读数只留计数，事后无法归因。

### 1.2 修法（两处，都是"通道"不是"判据取向"）

1. **`LOGDIR` 给一个可指定入口**：`LOGDIR="${GATE_LOGDIR:-$(mktemp -d)}"` ＋ 给定路径时 `mkdir -p`。
   照仓内既有的**负控制入口**惯例（`M134_SCAN_FILES`、`ANCH_SRCLIST`、`FRONTEND_DIR`）——
   默认行为一字不变，调用方能指定 ⇒ 全绿那一跑的名单从此留得住。
2. **行 6 外显 SKIP 清册 + "清册 ≡ 计数"自检**：在八个计数之后补
   `roster_top=`／`roster_sub=` 两格与逐条名字，并且**名单短于计数即把本行判 FAIL**。
   - ★ 读 `$LOGDIR/6-test.log` 的**每一处**都必须 `grep -a`——M404 的教训就是 BSD/GNU grep 的二进制判定
     会让"计数准、名单缺"，而我正要新增的恰好是"取名单"的那几处。
   - ★ 为什么不判"有 skip 就红"：合法环境性 skip 先例在册（`requireSymlinkSupport()` 一族），
     一刀切会把门禁训练成"大家都忽略的那一行"（M331 的取向，一字不改）。**判红的是"名单不全"，不是"有 skip"**。
   - 自检的数学：`roster_top ≡ top_SKIP` 且 `roster_sub ≡ sub_SKIP`；`--fast` 分支不取数、照旧 SKIP。
3. **行 14（`smoke-symlink-assert`）的下界话术同步**：新增判据 ⇒ `MIN_CHECKS` 与"走到断言 N 条"要一起重取。

### 1.3 改前红与归因（顺序不能反）

- **改前红**：`bash scripts/run-gates.sh` 的 stdout 里 `grep -c '^roster_top='` = **0**（只有计数）
  ⇒ 新通道本身在改前树不存在，其"修前必红"由**变异**顶（§9 MU-1/MU-2），不假装行为级红。
  这条限定照 §6.23 二·1 的规矩写死：**新缝的判据不可能是"改前红"**。
- **归因**（本条真正的目的）：修完后同窗各跑一次行 6 与包装脚本，取两份名单做 `comm` ⇒
  7 与 6 的差集就是那一条有名字的用例。**先修通道再做归因**（§6.72 八·M405 原话），本批照此执行。
- ★ 归因结果只写"差集是谁、为什么在两把尺子上不同"，**不预判它是缺陷还是环境差**；若归因指向
  "行 6 与包装脚本的取数口径不同"，那是**又一格通道缺陷**，按约束取新号登记。

### 1.4 归因实读数（设计段落笔后当场现取，不是预测）

同一棵干净树（HEAD `170a216`）、同一时间窗，两把尺子各跑一次：

| 取数 | 命令 | 结果 |
|---|---|---|
| A | `go test -count=1 -v ./...`（＝行 6 原样，日志 `/tmp/r674/row6_pre.log`） | `rc=0`、`top_SKIP=6`、`top_PASS=1034` |
| B | `bash scripts/ci-go-test.sh -count=1 ./...`（日志 `/tmp/r674/wrap_pre.log`） | `rc=0`、`skip_top=7 roster_top=7`、`pkgs_ok=23` |

差集（`comm -13` 两份去名字集）= **一条**：`TestClonePairDoubleCountsActualBlocks`
（`internal/realbytes/realbytes_clone_darwin_test.go:43`）——A 里 `--- PASS`、B 里 `--- SKIP`，
Skip 原因在**同一份日志的上一行**（`:310`）：

> `realbytes_clone_darwin_test.go:62: 量具自检不过：写 8388608 B 后可用空间只减 1880064 B（< 半份）——本环境不按字节记账，Δ 不可用`

⇒ **结论与 §6.72 的记录方向相反**：那一轮是"行 6 = 7、包装 = 6"，本轮是"行 6 = 6、包装 = 7"。
所以这不是**口径差**（两把尺子读的是同一个 `^--- SKIP`，且各自 `roster ≡ count` 都自证完整），
而是**跑次差**：那条用例的前提是"写 8 MiB 后卷可用空间要按字节掉"，而它用 `Statfs` 在两次调用之间
与**本机并发写盘**比大小（注释 `:47-49` 自己写了"Δ 用 int64：与测试机上的并发写盘比大小"）
⇒ 负载高时 `wrote` 掉到半份以下就合法 Skip。
★ 三件后果，逐条都要落到账里：
1. **M405 的问题答案是"没有口径 bug，是计数天生不可跨跑做差"**——§6.72 五·1 那格"必有一格本轮解释不了"
   到此解释完，且解释**不指向任何一层缺陷**。
2. **跨跑对账必须先剔这一条**（划账要写死：行 6 与 CI 腿的 `top_SKIP` 差 ±1 时先看它在不在名单里，
   不许直接读成"有一条用例回归/修复了"）。这属于新的、可复用的读数约束 ⇒ 取号登记（从 M406 起）。
3. 这**正是** M405 登记行预言的第三族（"某条用例在特定机器状态才 skip"），预言命中 ⇒
   登记行的价值在这里被一次真读数验收，不是靠我再说一遍。


---

## 2. M402 —— `build.yml` 的 `Run Go Tests` 仍不带 `-v`

### 2.1 现读基线

`grep -rn '^\( *\)-\{0,1\} *run:.*[^-]go test' .github/workflows/` ⇒ 全仓**只剩一处**：
`.github/workflows/build.yml:173` `run: go test -race -count=1 ./...`（linux＋macOS 两条腿共用）。
`grep -rc 'run: bash scripts/ci-go-test.sh'` ⇒ `ci.yml` = **4**、`build.yml` = **0**。
⇒ 发布腿是**不对称**形状：windows 腿经 `test-windows-quarantine.sh` 有 `-v`，另外三条腿分不出 PASS／SKIP。

### 2.2 改法

`run: go test -race -count=1 ./...` → `run: bash scripts/ci-go-test.sh -race -count=1 ./...`，
`if: matrix.platform != 'windows'` 一字不动。
⇒ 接线数从 4 升到 **5**（这就是 §3 那条锚的下界真值）。

★ macOS-intel／macOS-arm／linux 三条发布腿都已有先例：`ci-go-test.sh` 在 `macos-14` 腿上真跑过
（run `36850278294` 的 macos 腿 READOUT `skip_top=14 roster_top=14`）⇒ 不是新引入的未验证形状。
★ 兑现边界：发布腿只在 tag／dispatch 上跑，**本批 §5 那一跑正好覆盖它**（同一次 dispatch 里
`Run Go Tests` 三步会带着 READOUT 跑）；在此之前不得写"发布腿已能分 PASS／SKIP"。

---

## 3. M401 —— M331 的四处接线没有防回归锚

### 3.1 缺陷形状

谁把 `ci.yml`／`build.yml` 的 `run: bash scripts/ci-go-test.sh …` 改回裸 `run: go test …`，
**15 行门禁照绿**、SKIP 又变回读不出来。现有 E 组锚（`smoke-symlink-assert.sh`）只管
"`$VAR` 紧跟全角字符"，管不到这件事，且它的扫描面**含 `ci.yml`、不含 `build.yml`**（M177/M334）。

### 3.2 新锚（G 组，落在 `scripts/smoke-symlink-assert.sh`，不新增判定行）

两条判据，缺一即 bad：
1. **不许裸调**：`.github/workflows/*.yml` 里**代码行**（整行 `#` 注释跳过——`ci.yml:241`、
   `:250-251` 那些注释里就有 `go test -race -count=2 ./...` 的举例，不跳会误伤）
   匹配 `^[[:space:]]*-?[[:space:]]*run:.*[[:space:]]go[[:space:]]+test` ⇒ 命中即 bad，并把文件名＋行号打出来。
2. **接线数下界**：`grep -c 'run: bash scripts/ci-go-test.sh'` 在**两份** yml 里的总数必须 **≥ 5**
   （现值 5 ＝ ci.yml 4 ＋ build.yml 1，M402 的那一处）。
   ★ 为什么必须有第二条：只判"不许裸调"，则**删掉那五步**同样满足（`run:` 行没了 ⇒ 零命中 ⇒ 假绿）。
   这正是 M119／M108 反复点名的"下限自证"那一格。
3. **E 组扫描面一起抬**：`scan_list` 由 `scripts/*.sh + ci.yml` 改成 `scripts/*.sh + workflows/*.yml`（两份），
   下界随 §4 新增脚本一起重取（见 §4.3 的算术），并把 `:330` 那句"8 个 scripts/*.sh + ci.yml = 9"的话术改口。

### 3.3 兑现边界

- G 组是**静态锚**：它证明"文本形状没被改回去"，不证明"包装脚本真的能分 PASS／SKIP"（那一格由
  CI 三腿的 READOUT 与 §1 的本机行 6 名单负责）。
- ★ 变异 MU-3/MU-4 必做：① 把 `build.yml` 那一处改回裸 `go test` ⇒ G 组必须红在**判据 1**；
  ② 把 `ci.yml` 两步删掉 ⇒ 必须红在**判据 2**。红在别的格子＝锚没钉住。

---

## 4. D3 判红臂 —— 从"CI 上永远不会被执行"改成本机可实跑的常驻门禁

### 4.1 为什么现在是反事实

D3 的分支住在 `.github/workflows/ci.yml:150-194` 的**内联 `run:` 块**里（`case "$rc" in 0) … 124|137|143) …
2) … *) …`）。ubuntu 腿的 `sudo bash scripts/smoke-symlink.sh` 真挂得上 loop＋ext4 ⇒ **恒 rc=0**，
`2)` 那一臂在 CI 上一次都没被执行过；本机 harness 行 15 走的是 `sh_row`（rc=2 且日志有行首 `SKIP` ⇒ SKIP），
也不是那一臂。**"改对了"这一格至今没有读数**——因为它没有可调用入口。

### 4.2 修法：把那段 shell 收归仓内脚本（行为等价迁移，判据一字不改）

新增 `scripts/ci-smoke-symlink-gate.sh`：内容就是 ci.yml 里那段 `case` 逻辑（含 420s `timeout`、
`PIPESTATUS` 取码、`::error::` 与 `$GITHUB_STEP_SUMMARY` 落笔），**只是把两处环境耦合做成入口**：
- `SMOKE_GATE_CMD`（默认 `sudo bash scripts/smoke-symlink.sh`）⇒ 本机用桩驱动，不需要 root、不需要 mount；
- `SMOKE_GATE_SUMMARY`（默认落 `${GITHUB_STEP_SUMMARY:-…临时文件}`）⇒ 本机可断言"步骤摘要里写了什么"。
  ★ 为什么摘要必须能断言：D3 的价值有一半在"红的时候人看得见原因"，只断言退出码等于把那一半丢了。

`ci.yml` 的那一步改为 `run: bash scripts/ci-smoke-symlink-gate.sh`（`timeout-minutes: 10` 留在原位）。

### 4.3 常驻判据（同一次收归顺带补上的三格）

在 `smoke-symlink-assert.sh` 新增 **H 组**，用桩把四条臂各打一次并**逐臂断言 rc 与摘要内容**：

| 臂 | 桩给什么 | 必须读到 |
|---|---|---|
| rc=0 | 桩打 7 条判据样 stdout | 脚本 rc=0、摘要里**没有** "未通过" |
| rc=2 且有行首 `SKIP` | 桩 `exit 2` ＋ 打 `SKIP（跳过，非通过）:` | **rc=1**（D3：判红，不再与全绿同形）＋ 摘要点名"本轮未被执行" |
| rc=2 且无 `SKIP` | 桩 `exit 2`，不打自证 | **rc=1** ＋ 摘要走 M133 那句"按真失败判，不降级为跳过" |
| rc=124 | 桩 `exit 124` | **rc=1** ＋ 摘要点名超时（阻塞与判据不成立都要人来看） |

⇒ 这一组的意义正是：`2)` 那一臂**从此在本机每次门禁里都被执行**，反事实消失。
★ 三条纪律照旧：判红不等于通过（AS-K2）、`MIN_CHECKS` 与"走到断言 N 条"随新增条数一起抬、
新增一份脚本 ⇒ `docs/04` §7 的"门禁脚本 N 份"锚格必须同步（§6.71 撞过的行 12 假红，不再撞第二次）。

### 4.4 兑现边界

- 收归后 CI 的 ubuntu 腿只执行 **rc=0 那一臂**（真挂载成功）；四条臂的真读数以**本机**为准，
  CI 只作旁证（E 组同一口径，`bash 3.2` 那类缺陷也只在本机复现）。
- ★ 行为等价性怎么自证：迁移前后 `case` 分支的**判据与文案逐字对照**（`diff` 两段），
  并在划账里给出对照结果；不做"重写一遍更干净的"——那是把 D3 换成一个没被裁过的新判据。

---

## 5. M120 ＋ D1 三格 —— 一跑 `workflow_dispatch` 覆盖

### 5.1 先更正一条过期授权理由

台账与内存里三轮都写着"**dispatch 会连带建公开 Release ⇒ 未经同意不触发**"。现读 `build.yml`：
- `release` job：`if: startsWith(github.ref, 'refs/tags/v')` ⇒ dispatch（ref 是 `refs/heads/main`）**整个 job 跳过**；
- 即便 tag 触发也是 `draft: true`（J-1=(a)）⇒ 只建草稿，需人点 Publish。
⇒ **"dispatch 会建公开 Release"这一句在 J-1 落地后已不成立**，M120 的"要用户点头"剩下的实际代价只有
 runner 分钟数与 Actions 运行历史（对仓内可见、不对外）。用户本轮**点名 M120**，即为那格登记的"点头"。
★ 原文不抹平：这条过期理由在 §6.74 里连因由一起更正，并检查 `docs/04` 其余引用同一句话的格子。

### 5.2 一跑覆盖的四格

`timeout-minutes`（`verify-ci` 5／`build` 40／`release` 15＝M120）＋ D1 Step 1（`npm ci` 裸调）＋
Step 2（`go test -race`，经 §2 现在还是包装脚本）＋ Step 3b（`release` 的 `concurrency` 语法）。
四格的共同点是"**GitHub 自己才认**"——本地 YAML 解析只挡语法错。dispatch 一次 ⇒
GitHub 接受该 workflow（语法与字段有效）、作业跑起来并在 `timeout-minutes` 内完成 ⇒ 前三种形状拿到实读数；
`release` job 在 dispatch 下**被跳过**，所以 **Step 3b 那一格仍不兑现**（不假装：跳过≠验证语法）。
★ 严格说 GitHub 在 dispatch 时也会**校验整个文件**（含 `release` job 的 `concurrency` 块）——
校验通过只等于"语法被接受"，**不等于并发闸按预期串行**；那一格仍要等一次真双发 tag。

---

## 6. A2 —— `TestUndoHardlinkSelfHealsAfterUnlinkCrash` 的假前提 Skip

### 6.1 现读证据（三件，互相独立）

1. `internal/ops/undo_i6_test.go:136-138`：`if runtime.GOOS == "windows" { t.Skip("Windows 上 inode 身份未解析，走的是大小降级分支") }`。
2. 它**确实在 windows 腿上 Skip**：`/tmp/run_673_own.log:600` `--- SKIP: TestUndoHardlinkSelfHealsAfterUnlinkCrash`
   （windows 腿，`-count=1`；同一条在 `:678` 的名单里再出现一次）。
3. 前提已被推翻：同文件**紧邻的兄弟用例** `TestUndoHardlinkReplacedByThirdPartyStillBlocked`（`:167`）
   **没有** windows Skip，用同一批原语（`HardlinkMerge(keep, dup, fsid.ID{}, fsid.ID{})` ＋ 内容/身份复核），
   而它**不在** windows 腿那份完整名单里（`grep -a` 该日志零命中其名）⇒ 按 §6.73 判据链＝**在 windows 上跑过且绿**。
   ★ 名单完整性由 `roster_top=28 ≡ skip_top=28` 自证（M404 之后才谈得上"完整"）。

⇒ 那句 Skip 文案里"inode 身份未解析"已被 fsid I7（句柄身份）与 A1 那批读数共同作废。

### 6.2 改法与边界

- **删掉那个 `if` 块**（不改成"按平台分岔"，也不留"降级断言"）⇒ windows 腿上这条强校验路径**重新暴露**。
- ★ 风险如实写：本机是 darwin，删 Skip 后**没法在本地证它在 windows 会绿**。
  真正的读数只能来自推送后那一跑的 windows 腿——而按 §6.21 七，**本批自己触发的 run 不回写**。
  ⇒ A2 的 windows 兑现落在**下一批**引用本批 run 的日志时；本批只登记"代码已改＋darwin/linux 腿照旧绿"。
- 若那一跑它**红**了：那是 A2 的本意（暴露被 Skip 掩盖的路径），按新缺陷取号处理，不当夹具问题抹掉。

---

## 7. A3 —— pathpolicy 混例（盘符 / UNC / 尾分隔符 / 相对路径）

### 7.1 本格原文与现读不成立之处

`docs/05` §0.2 A3 写着"前端 pathpolicy 混例……是 node 用例，不必真机"。现读：
- `frontend/src/utils/pathpolicy.ts` **只剩一条判据** `isGroupCrossVolume`，输入是后端算好的卷标识
  （`vid:<id>` / `root:<VolumeName>`），**函数体不解析任何路径**（`:1-11` 的头注写着 D-1/AS-H6 那套
  路径判据已被删除，因为它是 `internal/ops/keep.go` 之外的第二份实现）。
- node 面**已有 8 条用例**（`frontend/tests/pathpolicy-crossvolume.test.ts` U-1~U-8），
  其头注 `:8-12` 已经把这件事说清：审查原文那四格是 **D-1 删代码之前**的考题，
  盘符那一格保留为 U-6（钉"连盘符形状也不许猜"）。

⇒ **按字面在 node 面补"UNC／尾分隔符／相对路径"混例是做不出真判据的**——那段代码已经不解释路径了，
写出来的断言只能钉住"字符串相等比较"，与那三种形状无关。这是**登记文本过期**，不是缺口。

### 7.2 本批对 A3 的处置（改指归属，而不是补假用例）

四格逐格给真去处，并用现读点名每一格有没有覆盖：

| 形状 | 现在的合法归属 | 覆盖读数（改前） |
|---|---|---|
| 盘符 | `pathpolicy-crossvolume.test.ts` U-6 | ✓ 在 |
| UNC | `internal/pathnorm/pathnorm_test.go:39` 形状表里有 `"\\\\"`（反斜杠根） | △ **只有根、没有 `\\server\share` 全形** |
| 尾分隔符 | `TestTrimTailKeepRootMatchesSysguardNormalize`（`:74`） | ✓ 在 |
| 相对路径 | — | ✗ **零命中**（形状表里全是绝对形） |

⇒ 本批做两件**小**事：① 给 `pathnorm_test.go` 那张形状表补 `\\server\share` 全形与相对形（`a\b`、`./a`、
`..\a\b`）几格，钉的是 `Slash`/`DirKey`/`TrimTailKeepRoot` 对它们的**不解释**（不吞根、不猜绝对）；
② `docs/05` 的 A3 格按"混例已改指后端 `internal/pathnorm`，前端那条只剩卷标识相等"销格。
★ 明确不做的：不给 `isGroupCrossVolume` 加"盘符怎么比"的断言（那会把 D-1 删掉的第二份实现请回来）。

---

## 8. `docs/05` 其余真机格

逐格判定在划账 **§6.74 一-B**（每格给：能否由 CI 三腿读数兑现、已由 §6.73 判据链证到哪、
剩下必须真机的缺哪一件硬件／权限）。本设计段不预写结论——**没读过的格不许先写状态**。

---

## 9. 探针与变异清单（本批自己的号，裸号只在"变异"二字之后出现）

| 编号 | 内容 | 预期 |
|---|---|---|
| P-65 | 改前红：`bash scripts/run-gates.sh` stdout 里 `^roster_top=` 零命中 | 通道本身缺（红） |
| P-66 | 改后：`GATE_LOGDIR=/tmp/…` 指定入口生效、全绿跑的 `6-test.log` 与名单**留得住** | 新通道绿 |
| MU-1 | 把行 6 的 `grep -a` 换成 `grep`（回到 M404 的缺陷）⇒ 清册若被截断必须**判红本行** | 必须红 |
| MU-2 | 把行 6 名单外显那几行删掉 ⇒ `roster_top` 不等计数？不——删掉后 `roster_top=` 整行消失，判据必须把"没有 roster 行"本身读成 FAIL（**不许读成 0≡0**） | 必须红 |
| MU-3 | `build.yml` 那一处改回裸 `run: go test` ⇒ G 组判据 1 红 | 必须红 |
| MU-4 | 删掉 `ci.yml` 两步包装 ⇒ 接线数 3＜5 ⇒ G 组判据 2 红 | 必须红 |
| MU-5 | 把 `ci-smoke-symlink-gate.sh` 里 `2)` 臂的 `exit 1` 改成 `exit 0` ⇒ H 组两臂同红 | 必须红 |
| MU-6 | 把"无 SKIP 自证"那一臂并到"有 SKIP"那一臂（还原 M133 改前） ⇒ H 组第三条红 | 必须红 |
| MU-7 | 删掉 `TestUndoHardlinkReplacedByThirdPartyStillBlocked` 的现有覆盖（临时）⇒ 不成立即不测，本条只做"兄弟用例在场"的静态核对 | 记录用 |

★ 变异跑完必须把"设计段预测的包名/断言名集合"与"实测集合"列出来相减（§6.9 M64 立过的口径），
别只记"杀了几条"。

---

## 10. 交付面与节律

1. 设计段（本文件）→ 提交。
2. 实施笔（可多笔，按 M405 / M402＋M401／M177 / D3 收归 / A2 / A3 分格提交，每格带自己的读数）。
3. 划账 **§6.74**：七格落位表 ＋ §6.74 一-B 的 `docs/05` 逐格判定 ＋ 三个自洽式 ＋ 门禁终态逐行读数
   ＋ 兑现／未兑现清单 ＋ 自纠。
4. 收口：提交 → `git push` → 盯 CI 三腿 → 一次 `build.yml` dispatch（§5）→ 读数只报给用户。
