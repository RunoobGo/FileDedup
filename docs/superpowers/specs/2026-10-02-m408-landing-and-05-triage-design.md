# 2026-10-02 设计段：M408 的 runner 兑现格落账 ＋ docs/05 真机家族分诊（本机可执行的两格先取数）

出处：用户选中 §6.77 末段那句"下一批接着做"，并对"要不要连 3b-β 的 tag 演练一起做"选定
**「不动 tag（推荐）」** ⇒ 本批**零对外动作**（不打 tag、不发 dispatch、不改 workflow）。

## 0、范围与号

**(A) 落账批**（纯文档）：把上一批**自触发**的两个 run 的读数按 §6.21 七 落进 `docs/04`——
run `36887908248`（push CI，head `0f889e8`）与 run `36887965817`（build.yml `workflow_dispatch`，同 head）。
落三格：**M408 的 runner 半边**、**§6.77 七·2 的半边**（`::error::` 与退出码的关系）、
**D1/M402 在最新 SHA 的四腿读数**，外加 **M406 一般式的第三次应用**。

**(B) 真机取证批**：`docs/05` 的 MAC 家族里**能在本机（darwin 真机）无损执行**的格先取数
（MAC-1 的可自动判那几半 ＋ MAC-2 的"外部 `TMPDIR` 到不到得了锁文件"那一前置自检），
其余逐格写明**为什么这台机器这一批取不到**。

★ **本批预计不占号**（兑现格与分诊都不是缺陷）。若 (B) 执行中真读出缺陷 ⇒ 占 **M409**；
按本仓第七次应用的那句话：**拟号不等于分配，分配在复核之后**。

## 一、能力现读（决定 (B) 能在哪儿取数，全部本批现跑）

1. **本机就是一台 darwin 真机**（macOS 27.0 / arm64，GUI 登录会话）⇒ MAC 家族原则上可达，代价与侵扰性逐格判。
2. **`wails` CLI 在位**：`/Users/just/go/bin/wails`。旧产物 `build/bin/FileDedup.app/Contents/MacOS/FileDedup`
   mtime **2026-09-28 19:45:21**，而最新**非测试** Go 提交是 `bf45044`（**2026-10-01 13:12:12 +0800**）
   ⇒ 旧二进制**不配**当本批读数的载体。本批用 `wails build -platform darwin/arm64 -s` 重建
   （`-s` ＝ 跳过前端生成，`frontend/dist` 现读 mtime 2026-10-01 23:45:24 是门禁刚产的那份），
   新产物 mtime 落在本批取证时刻、大小 13 609 312 B（旧 13 526 512 B）。
3. **本机没有任何 Linux 宿主**：`docker` / `podman` / `colima` / `lima` / `limactl` / `orbctl` / `utmctl` /
   `qemu-system-aarch64` **八个命令全 `none`**，`docker version` → `command not found`。
   ⇒ **LNX-1 ②③／LNX-2／LNX-3／LNX-4／LNX-5／LNX-6／LNX-7 ②④ 这一族本批一条也取不到**，
   性质是**没有宿主**、不是"没去做"。唯一现成的 linux 现场是 CI 的 ubuntu 腿，而它要**往 workflow 里加一步**
   才走得到这些判据 ⇒ 属另一批（且要单独对齐"CI 腿绿不折算成真机读数"那条 §0.1 约束怎么用）。
4. **Windows 侧**：本机无 Windows，用户另有 Windows 会话（04 §6.54~§6.56 的跨机双线在册），不属本代理可达面。

## 二、(A) 的读数坐标（本批已现取，落账逐字照抄）

**run `36887908248`**（push，head `0f889e8`，`gh run view --json jobs`：三 job 全 `completed/success`；
watch rc=0 落盘 `/tmp/ci_677_watch.rc`；job 日志 `/tmp/ci_677_check.log` 共 2822 行）：

- 行 **2714** 桩输出 `SKIP（跳过，非通过）: 本行是 M408 自证桩，不是真挂载失败现场`
- 行 **2715** `##[error]跨卷软链接冒烟被跳过（runner 无法挂载独立文件系统）——该防线本轮未被执行；按 D3 裁定判红，不再与全绿同形`
- 行 **2716** `OK(M408): 判红臂在 runner 上交回 rc=1，且文案真的进了本次 job 的步骤摘要`
- 行 **2777** `✓ ci.yml 调用门禁脚本 2 次 ≥ 2（真跑 ＋ 判红臂自证都在，M408）`
- 行 **2780** `smoke-symlink-assert: 走到断言 34 条（下限 33 条），其中失败 0 条`

**七·2 那半边的判据形状**：同一步里 `##[error]` 注解**已打出**（2715），而该步随后交回 **rc=0**（2716 是最后一条 echo）、
整个 job `success` ⇒ **"注解本身不改退出码"从推理升为读数**。★ 仍**不闭合**的是另一半：门禁步骤**自己 rc=1** 时作业会不会真红
——那是反事实，要等一次 runner 真挂不上 loop；本格继续挂着，**不许写成已验证**。

**run `36887965817`**（build.yml `workflow_dispatch`，同 head；`Create GitHub Release` = **skipped** ⇒ 无对外发布发生）：
四条 Build 腿各一条 `M331_READOUT`，全 `rc=0 pkgs_ok=23 fail_top=0 fail_sub=0`、`roster ≡ count`：
macos-arm64 `skip_top=7/roster 7`、macos-amd64 `7/7`、windows `27/27 ＋ sub 1/1`、**linux `3/3`**；
`npm ci` 四腿各命中（`added 50/51 packages`）。

★ **M406 一般式第三次应用**：linux 腿从上一发（`36872056147`，head `f74da1b`）的 `4/4` 变成这发的 `3/3`。
**先比名单**才看清：少的正是 `TestHardlinkMergeRejectsReplacedKeep`（这发它跑且过）——
两发的名单（各自 `roster ≡ count`）现读为
旧 `{ApplyProcessPolicyCaseFold, HardlinkMergeRejectsReplacedKeep, PickInDirectoryTolerantOnInsensitiveVolume, RevealPathWindowsExplorerNoiseEndToEnd}`、
新 `{ApplyProcessPolicyCaseFold, PickInDirectoryTolerantOnInsensitiveVolume, RevealPathWindowsExplorerNoiseEndToEnd}`。
⇒ §6.77 二 里那句"linux-amd64 `4/4`"**不必更正**（它引的是另一发，点名点齐了），但本批要写明**两发是两个不同 host 形态**，
窄口径（只比数字）会把这条读成"用例消失"。

## 三、(B) 的执行计划（逐格写清判据、侵扰性与安全边界）

**先钉框架侧原文**（本批从模块缓存现读 `github.com/wailsapp/wails/v2@v2.16.0/internal/frontend/desktop/darwin/`，
按 M150 口径只钉符号不钉行号）：`SetupSingleInstance` 走 `createLockFile(getTempDir() + "/" + uniqueID + ".lock")`，
`getTempDir()` 调 `C.GetMacOsNativeTempDir()` → **`NSTemporaryDirectory()`**；`createLockFile` 的
**open 失败**与 **flock 失败**合在同一个 `if err != nil` 出口 ⇒ `SendDataToFirstInstance` + **`os.Exit(0)`**；
open 失败那条打印 `Failed to open lockfile %s: %s`，flock 失败当 `err` 含 `resource temporarily unavailable`
时**刻意不打印**（"别的实例已占"这一档是静默的）。⇒ MAC-2 的"预期症状"到此是**代码级确证**，
本格要补的是**真机那一跑**（尤其"外部 `TMPDIR` 到底进不进得了 `NSTemporaryDirectory()`"这一条，框架代码答不了）。

1. **MAC-1（单实例合并）**——本批只做**能自动判的那三半**，全程终端起、终端杀，不留窗口：
   ① 首实例起（会短暂开一个窗口，几秒内 `kill`）；② 同参第二实例**立刻退出且 `echo $?` ＝ 0**；
   ⑤ `kill -9` 首实例后再起一次，必须**正常起**（残留 `.lock` 不挡，`flock` 问持有者不问在不在）；
   ⑥ 跨账户那一档本机取不到（要第二个登录账户），**明写不销**。
   ★ ②③④ 的"窗口被带到最前／从 Dock 恢复／合并提示文案"需要**人眼或 System Events 授权**（会弹用户可见的
   自动化授权框）⇒ **本批不动用户屏幕上的授权**，记"半兑现"，绝不把整格写成已过。
2. **MAC-2（`$TMPDIR` 不可写 ⇒ 静默不启动）**——本批先跑**前置自检**那一格：用一次性可写目录
   `D=$(mktemp -d)` ＋ `TMPDIR="$D"` 起一次，`ls -l "$D"/*.lock` 看锁文件**是否真落进外部 `TMPDIR`**。
   - 落了 ⇒ 再 `chmod 500` 一个新目录复跑，取"无窗口／无错误框／rc=0／stdout 那行"四件齐的正式读数，**本格销**。
   - 没落 ⇒ 只剩清单里那条备选法"把**真实** `$TMPDIR` `chmod 500`"，而那会**同时影响同机其它正在跑的进程**
     （它们的新建临时文件会失败）⇒ **本批不做**（不拿系统级副作用换一格读数），记"① 前置自检已取到、
     判据本体待一次性测试账户或用户在场时手工执行"。
3. **MAC-3（以 `/` 为根真扫的耗时）**——判据 ④ 要的是**全量真扫耗时**，代价是长时间占 I/O（本机工作目录在
   外置卷）＋可能触发 TCC 隐私弹窗 ⇒ **交用户决定何时跑**，本批不擅自起。★ 这不是"没做"，是"侵扰性需点头"。
4. **MAC-4**（iCloud dataless 非 0 字节正例）：清单原文写死**需用户在场并同意下载配额** ⇒ 本批不动。
5. **MAC-5／MAC-6**（真废纸篓分批/超时/行协议、`-` 开头文件名走 GUI 全链路）：判据落在**经 Wails 窗口与 Finder 的
   全链路**那一层 ⇒ 需人操作 GUI（或 computer-use 驱动），本批不做，明写。
6. **LNX 全族**：见一·3（无宿主）。

## 四、本批不做清单（下一轮不许当"已覆盖"读）

产品代码零改动；`.github/workflows/` 零改动；**不打 tag、不发 dispatch**；不往 CI 加 linux 步骤；
不动用户的 TCC／自动化授权；不 `chmod` 真实 `$TMPDIR`（系统级副作用）；MAC-3／4／5／6、LNX 全族、
W 系列（Windows 真机）一条不动。★ 3b-β 仍挂 `docs/05` W10-4 ④ 等 tag；`::error::` 的"步骤 rc=1 ⇒ 作业真红"
仍挂反事实；M186 与 `TestVerdictCtxHonorsDeadline` 继续挂账；M75(b) 仍未裁定。
