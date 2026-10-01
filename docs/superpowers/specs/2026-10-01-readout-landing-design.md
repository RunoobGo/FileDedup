# 2026-10-01 兑现格落账批 ＋ M408（D3 判红臂的 runner 侧自证）设计段

出处：用户指令「逐一完成未兑现任务：M405、M401/M402、M120、D3 的判红臂（ubuntu 腿恒 rc=0，反事实）、docs/05 其余真机格与 A2/A3」的收尾批。§6.74 把七格都给了下落，并把"只能来自下一批引用"的格子逐条写死；本节就是那个"下一批"。

## §0 范围与编号

两半，互不遮挡：

- **(A) 兑现格落账**：六格，**零代码、零脚本改动**，引的全是**上一批**两个 run 的日志读数（见 §1、§2 的资格论证）。落账位 `docs/04` §6.77。
- **(B) M408**：一格真缺件——D3 的判红臂**在 runner 上从没发出过文案**。改法是 `ci.yml` 的 linux job 加一步"判红臂自证"（用桩强制走 `2)` 那一臂，摘要入口**故意不设**，逼它走 M407 刚修的 `GITHUB_STEP_SUMMARY` 默认支），外加 `smoke-symlink-assert.sh` 的 **J 组两条静态锚**。★ **M408 是拟号**，分配在划账号表那一刻（同一纪律第六次应用）；**下一自由号 M409**。

本批不动产品代码、不动 `build.yml`、不 tag。

## §1 六格兑现的坐标（全部现读，逐条给日志行号）

两个 run 的身份先钉清：

| run | 触发 | head | 含哪批改动 |
|---|---|---|---|
| `36870525837` | push | `f74da1b` | §6.74 七格 ＋ §6.75 M407 修复 |
| `36872056147` | `build.yml` `workflow_dispatch`（本会话 13:53:21Z 亲手发的） | `f74da1b` | D1 三格 ＋ M402 |

1. **§6.75 六·(a)「ubuntu 腿真挂载 ＋ 七条判据成立」** ⇒ `/tmp/ci_407_clean.log` 行 **4079** = `##[group]Run bash scripts/ci-smoke-symlink-gate.sh`，行 **4086-4087** `==> 尝试 loop + ext4 独立卷` / `✓ 已挂载 ext4（/dev/loop0）`，行 **4088** `卷 A dev=2049 ｜ 卷 B dev=1792`，① — ⑦ 逐条 `✓`，行 **4114** `跨卷软链接 7 条判据全部成立`。⇒ M407 修的就是这一步的第一步（`mktemp -t`），现在它**走到了判定并交回 rc=0**：红因消除，在 GNU 腿上是**实测**不是推断。
2. **行 14 等价物（`冒烟脚本判据自证`）在 ubuntu 上真跑** ⇒ 同日志行 **4115** `##[group]Run bash scripts/smoke-symlink-assert.sh`，行 **4168** `== I 未指定 SMOKE_GATE_SUMMARY 时的落点与临时文件回退（M407：run 36866748734 的红因）`，行 **4174** `smoke-symlink-assert: 走到断言 32 条（下限 31 条），其中失败 0 条`，组名清单里含 `H 四臂判红` 与 `I 默认摘要入口`。⇒ §6.75 四 那句"MIN_CHECKS 27→31、本机实跑 32"在 **GNU 腿同值**。
3. **A2 的 windows 读数** ⇒ run `36870525837` 的 `go test (windows)` 腿：行 **4874** `M331_READOUT rc=0 pkgs_ok=23 skip_top=27 skip_sub=1 roster_top=27 roster_sub=1 fail_top=0 fail_sub=0`；行 **4820** `ok  	filededup/internal/ops	3.114s`；`TestUndoHardlinkSelfHealsAfterUnlinkCrash` 全日志 **零命中**（`grep -ac` 通道取的真零；★ 包装脚本 stdout 刻意 `sed` 掉 `=== RUN`／`--- PASS` 四类噪声、只留包级行与 SKIP 清册，所以"名字不出现"的正确读法是"它不在这 27 条 Skip 名单里"，不是"它没跑"）。按 §6.73 判据链（包 `ok` ∧ `fail_top=fail_sub=0` ∧ 不在自证完整的清册里）⇒ **这条用例在 windows 腿跑过且绿**。★ §6.74 四 留的那句"它若红了是 A2 的本意"没有发生。
4. **M402（build.yml 改指包装）** ⇒ run `36872056147` 四条 Build 腿的 `Run Go Tests` 步：命令原文 `##[group]Run bash scripts/ci-go-test.sh -race -count=1 ./...`（macos-arm64 / macos-amd64 / linux-amd64 三腿逐字同形，windows 腿走 `test-windows-quarantine.sh`），四腿各打一条 `M331_READOUT`：macos-arm64 `skip_top=7 roster_top=7`、macos-amd64 `7/7`、linux-amd64 `4/4`、windows-quarantine `skip_top=27 skip_sub=1 roster_top=27 roster_sub=1`，四条 `rc=0 fail_top=0 fail_sub=0`。⇒ "包装接线数 ≥5"里 build.yml 那一处**在 runner 上被走到了**。
5. **D1 Step 1（`npm ci`）＋ Step 2（`-race`）** ⇒ 同 run：`Build Frontend` 步逐腿 echo 出 `npm ci`（改前那行 `if [ -f package-lock.json ]; then npm ci; else npm install; fi` 只作为注释出现）并 `added 50 packages, and audited 51 packages`；`-race` 读数即 §1.4 那四条 rc=0。⇒ 原"代码已改、CI 验证未兑现"两格转兑现。
6. **M120 / D1 Step 3b-α** ⇒ 同 run 结论：`Verify Tag Commit Passed CI` success、四条 `Build - FileDedup-*` success、`Create GitHub Release` **skipped**。⇒ ①`timeout-minutes` 5/40/15 三处声明**没有一个 job 超时**（弱兑现：只证"没被它咬死"，不证"超了会判红"）；② job 级 `concurrency` 的 YAML 与 `${{ github.ref }}` 插值**被 GitHub 接受**（整个 workflow 被解析并跑起来，release job 以 `skipped` 出现而不是以解析错误出现）。★ **3b-β（真串行）不销**——它挂在那 job 的 `if:` 门槛上，dispatch 结构上进不到（§6.76 三），只有 tag 演练能读，已挂 `docs/05` W10-4 ④。

★ **§1.3 那句"零命中"是复核过才写的**（M404 的口径在本批的正确用法）：`grep -ac` 取到 0；`perl -ne '/\0/'` 现读该日志**没有 NUL**；`grep -n` 带 `-a` 与不带 `-a` 对 `M331_READOUT` 给出同一组行号（933/1373/3618/4051/4874）⇒ 这份落盘没有"二进制吞行"的污染，零命中是**真零**而不是通道产物。（过程中确实红过一次 grep：`ok **filededup` 里那两枚 `*` 让 BRE 报 `repetition-operator operand invalid`、整条命令没输出——那是**我自己的正则错**，一度被读成"M404 又抓一枚"，现读后作废。★ 立话：**grep 空输出先看有没有 stderr，再论"零命中"。**）

## §2 为什么这些读数现在写得进账（资格论证）

`§6.21 七` 的回填边界判据是**"这个 run 由哪一批提交触发"**，不是"第几轮"。两个 run 都由 §6.74／§6.75 那两批触发 ⇒ 对本批（`docs/04` §6.77）而言是"上一批的读数"，**可引、可落格**。反过来，本批自己触发的两个跑（push 后的 `ci.yml`、M408 那一步的第一跑）读数照旧**只报给用户、不落本批**。这条对称性每次都容易被读歪，所以写在落笔处而不是只在号表里。

## §3 M408：判红臂在 runner 上发得出文案（一格从"不可观测"改成"有实物见证"）

### §3.1 缺陷面（现读，不是猜）

§6.75 六·(b) 当时把这一格写成"绿跑里**结构上取不到**"。现读 `scripts/ci-smoke-symlink-gate.sh:60-88` 复核该结论成立：`SUMMARY` 只在 `124|137|143` 与 `2)` 两臂被写，`0)` 那一臂只 `echo` 到 stdout ⇒ 正常绿跑对 `$GITHUB_STEP_SUMMARY` 的贡献是**零字节**。H 组四臂与 I 组①②确实各打了 `2)` 那一臂，但喂的是 `SMOKE_GATE_SUMMARY=$STUBS/….summary` ——**那证的是脚本逻辑，不是"runner 上那份摘要文件真能被发布"**。⇒ 于是这一格既不该记"未兑现"（那是"没做"），也不该记"已兑现"（那是"做过但没读数"）——它是**缺一枚只能在 runner 上跑的自证**。

### §3.2 改法

`.github/workflows/ci.yml` linux job，紧贴真跑那步（`:160-162`）之后加一步：

- 桩：`printf` 出 `SKIP（跳过，非通过）: …M408 自证桩…; exit 2` 落到 `$RUNNER_TEMP/d3-red-stub.sh`，`SMOKE_GATE_CMD="bash $STUB"` 喂进门禁脚本（**照 H 组 `arm()` 同一形状**：桩写成文件而不是内联引号串，`$CMD` 在脚本里是非引号展开，内联引号会被拆词拆坏）。
- ★ **不设 `SMOKE_GATE_SUMMARY`** ⇒ 强制走 M407 那三支选取的第二支（`GITHUB_STEP_SUMMARY`）。这一步的全部价值就在"用 runner 那份"，设了入口就自废。
- 断言两格：门禁脚本交回 **rc=1**（不是 0、不是 2）；`$GITHUB_STEP_SUMMARY` 里 `grep -qF '跨卷软链接冒烟未通过'` 命中。两格任一不成立 ⇒ 本步骤 `exit 1`。
- 步骤自己成功时 `exit 0`，作业不被自证拖红。

`scripts/smoke-symlink-assert.sh` 加 **J 组两条静态锚**（防"某次清理把这一步删了，防线静默消失"）：
- **J-①**：`ci.yml` 的**非注释行**里 `bash scripts/ci-smoke-symlink-gate.sh` 出现次数 **≥ 2**（真跑 ＋ 自证；注释跳过规矩沿用 E/G 组同一把尺）。
- **J-②**：`ci.yml` 里存在**同一行**同时含 `GITHUB_STEP_SUMMARY` 与 `未通过` 的行 ⇒ 证明自证那一步**自己核了文案落点**，不是只核 rc。
- `MIN_CHECKS` **31 → 33**（沿用"本机现跑值 − 1"那道闸；J 组整段被删 ⇒ 32−2=30 < 33 会红）。收尾那行组名清单补 `J 判红臂 runner 自证锚`。

### §3.3 为什么不用 `continue-on-error: true`

那等于"这步红了也算绿"——正是 D3 本轮推翻的 M8/M133 那个定性（"环境受限不该怪代码"）。自证步的判据如果不成立，说明 D3 那一半文案通道**真的坏了**，就该红。代价说清：红的时候 runner 日志里会同时出现门禁脚本自己 echo 的 `::error::`（预期，不是事故）。

### §3.4 兑现边界（落笔时就写死）

1. 这一步证的是**"文案发得出去"**，**不是**"runner 真挂不上 loop"——后者在本仓不可造（ubuntu 腿恒挂得上），也不该造。
2. 门禁脚本内联 echo 的 `::error::` 会进这次 run 的 Annotations。步骤 rc=0 ⇒ 作业结论不变；**这条要在本批第一跑用 `gh run view --json jobs` 封口**，不许凭"标准行为"三个字写进账。
3. 本机 darwin 只能代证 J 组锚（改前 ci.yml 只有 1 处调用 ⇒ J-① 天然红，属"改前必红"的合法形状）；**runner 半边只能来自本批自己触发的 run** ⇒ M408 的兑现格落**下一批**，本批只落"已实施＋本机锚"。
4. I 组①②（桩摘要入口）与本步不是同一格，两者都要有：前者证逻辑、后者证发布通道。

## §4 变异计划（两枚，还原后复跑绿）

| 枚 | 变异 | 预期红 |
|---|---|---|
| **MU-5** | 删掉 `ci.yml` 自证那一步的 `run:` 体里 `grep -qF` 那半行（把文案落点断言摘光） | **J-②** 红：`ci.yml` 不再有"同一行含 GITHUB_STEP_SUMMARY 与 未通过"的行 |
| **MU-6** | 把整步 `name` ＋ `run` 从 `ci.yml` 删掉 | **J-①** 红：调用次数 1 < 2 |

★ 不打算给"脚本本身判红臂坏掉"补新变异——H 组 `arm skip2` 已经钉住那条（摘掉 `2)` 那一臂 ⇒ 退出码格红），本批不重复取证，只把**runner 发布通道**这一格补成可观测。

## §5 本批不做的

产品码；`build.yml`；`docs/05` 的 GUI／注册表／第二物理卷／杀软／OneDrive／第二账户／tag 那一族真机现场（本机无该环境，AS-K2）；3b-β 串行窗口（要一次 tag 演练，`draft: true` 仍属对外动作，等用户点头）。
