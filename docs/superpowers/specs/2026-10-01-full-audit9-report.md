# 第九轮全面审查报告（2026-10-01）

## 0. 本轮定位与纪律

- 本轮**只出报告**：不改产品代码、不改测试、不改文档正文，**不占 M 号**。
  依据既有纪律「拟号 ≠ 分配，分配在 04 号表」，本报告里出现的都是**描述**，没有一个是已分配的登记号。
- 分级只到「建议」：每条附**可复现的取证命令**与**现读坐标**；取向由用户裁定，代理不选边。
- 报告只覆盖下面「二、覆盖面」明写的六路；未覆盖的面在「六、覆盖面边界」显式记账，**不得**被读成"全部审过"。

## 一、基线读数（取证起点，非本轮产出）

| 项 | 读数 |
|---|---|
| `HEAD` | `ccdea3d`（＝`origin/main`，第八轮裁定回收批已收口） |
| `gofmt -l` | 空，rc=0 |
| `go build ./...` / `go vet ./...` | rc=0 |
| `go test ./... -count=1` | **23 包全 ok**（`filededup` 12.989s、`cache` 6.638s、`dedup` 4.138s、`history` 2.314s、`ops` 1.496s …） |
| 门禁第 16 行（上一批冻结后的现读，`docs/04:9259`） | `anchors=958 src_files=492 EXACT=73 NEAR=139 DRIFT=280 NOSYM=371 MISSING=37 OOR=58 hit_rate=43.1%` |

日志留档：`build/m7scratch/audit9-baseline.log`。

⇒ **本轮没有发现 P0**（六路独立取证 + 主代理逐条复核，无"数据被销毁/被静默写错且必然触发"级别的新缺陷）。

## 二、覆盖面与取证方式

六路子代理（只读取证）＋ 主代理逐条复核：

| 路 | 范围 |
|---|---|
| A | `internal/ops/*`（执行层：移动／回收站／回撤／守卫／账本） |
| B | `internal/dedup/*` + `internal/scanner/*`（扫描与算法） |
| C | `internal/cache` `internal/history` `internal/model` `internal/fsid`（数据层） |
| D | `app*.go`（App 层：互斥、生命周期、记录页 IO、设置） |
| E | `frontend/src/*` + `frontend/tests/*`（前端与判据） |
| F | `docs/*` + `scripts/*` + `.github/workflows/*`（文档与门禁一致性） |

**复核方式（第七轮的教训落到操作上）**：子代理的**每一条负向断言**（"全仓没有 X""Y 没有覆盖"）由主代理自己跑 grep 取原始输出；坐标一律现读文件长度后判断可达性。本轮六路全部返回（无 429 丢失）。
**结果**：本轮复核**证实**了下面 §三 §四 全部条目，**推翻或归为在册**的条目单列在 §五——没有一份报告被整份采信。

## 三、P1 建议（判据/账实层面会产出错误结果，本机可复现）

### P1-1 回撤两条腿都不问 `maintaining`，而注释自称"双向闭合"

- 坐标：`app.go:398-412`（`claimMaintenance` 三问 + 置位）、`app.go:377-378`（"★ 双向闭合：维护先占住则扫描/**清理**被拒"）、`app_ops.go:290-305`（`UndoOperation` 锁内只查 `opsRunning`/`scanInFlight`）、`app_ops.go:403-420`（`UndoOperationItem` 同形）。
- 现读证据：`grep -n "maintaining" app*.go` 的全部命中是 `app.go:291/300/383/405/406/408/415`、`app_ops.go:55/56`、`app_scan.go:59/60` —— **两个 Undo 函数体内零命中**。
- 危害链（不是"可能慢一点"，是账实分叉）：维护（`ClearOpRecords`，`app_history.go:317`）在途时回撤照样起跑 → 整表删除把 `op_items` 抽走 → `MarkItemUndo` 的 `UPDATE ... WHERE id=?` 命中 0 行 → `internal/history/oplog.go:149-151` 返回「操作条目不存在」→ 走 `app_ops.go:393-394` 的 `warnLedger`（原文案：「文件已还原但记录未更新，历史页可能仍显示为可回撤」）。**文件已经回到盘上，账本条目已经不存在**。
- 这条危害 M364 已经在册（那次修的是 `ExecuteOperation` 腿），本条报的是**同族第二腿没修**，且 `app.go:377` 的"双向闭合"话术现在**过限**。
- 取证命令：`grep -n "maintaining" app.go app_ops.go app_scan.go`；`sed -n '377,378p;398,412p' app.go`；`sed -n '290,305p' app_ops.go`。

### P1-2 「清空全部历史」是四层防线全缺的唯一一个破坏性控件

- 坐标：`frontend/src/views/RecordsView.vue:50-54`（`clearAll()`：`confirmClear` 在 `await` **之前**复位）、`RecordsView.vue:289`（`<button class="btn-danger" @click="clearAll">` 是全文件**唯一**没有 `:disabled` 的危险按钮）、`frontend/src/stores/scan.ts:475-483`（`clearHistory()` 不经 `guard()`，对照 `:534-542` 的 `clearOps()` 有 `guard('清空清理记录')`）、`app_history.go:118-147`（`DeleteScanHistory` / `ClearScanHistory` 函数体内**没有任何门禁**；该文件的门禁只出现在 `:52` 与 `:95`）。
- 对照形状（证明这是遗漏而非取向）：同仓其它危险控件都有 `:disabled` —— `RecordsView.vue:302`（`:disabled="store.busy || clearingOps"`）、`SettingsView.vue:138`、`ResultView.vue:455`、`ConfirmDialog.vue:173`。
- 危害：双击 = 两次 `ClearScanHistory`；扫描/清理进行中也能清空历史（历史行是回撤与"从历史恢复"的上游依据）。M365 那批给清理记录补了 `claimMaintenance`，**没有外扩到这第四枚**。
- 取证命令：`grep -n "btn-danger" frontend/src/views/RecordsView.vue`；`grep -n "guard(" frontend/src/stores/scan.ts`；`sed -n '112,150p' app_history.go`。

### P1-3 `ReclaimActual` 约定 `files[0]` 是保留项，而默认保留判据是 `pickShortest`

- 坐标：`internal/model/model.go:165-177`（`ReclaimActual(files)` 对 `files[1:]` 求和，注释写"约定 files[0] 为保留项"）、`internal/dedup/pipeline.go:612-614`（`sortEntries` 只按 `Path <` 字典序）、`internal/ops/keep.go:97-111`（`pickShortest`：先非隐藏、再最短路径 ⇒ **与字典序不同一条**）。
- 危害：组内各成员实占不相等时（块对齐/压缩/预分配差异，正是 M6-P2 引入实占的理由），"可释放实占"按**错的那条**算 ⇒ 少算或多算。逻辑口径同一条求和也受影响，但逻辑大小组内通常相等，所以**实占那一列才是真会错的**。
- 暴露面（现读，不外推）：`cmd/fdd-cli/main.go:175/184/225`、`app_result.go:60/70/74/87`、`internal/history/scan.go:251`（写进 `scan_history` 的行 ⇒ **落库后不会随修复回填**）。界面当前不渲染它（`docs/04:1832` 明写"当前界面仍只显示逻辑口径"）。
- 取证命令：`sed -n '160,180p' internal/model/model.go`；`sed -n '95,112p' internal/ops/keep.go`；`grep -rn "ReclaimActual(" --include='*.go' . | grep -v _test`。

### P1-4 `PruneScanFiles` 重写了 `scanKey` 的三列 ⇒ 修剪后再导入会造重复扫描行

- 坐标：`internal/history/import.go:207-218`（`scanKey{SavedAt, Roots, Filters, GroupsCount, FilesCount, Threads, Paranoid, OrigFiles, Reclaimable}` —— 九字段里含 `GroupsCount/FilesCount/Reclaimable`）、`internal/history/scan.go:363-367`（`PruneScanFiles` 内 `UPDATE scan_history SET groups_count=?, files_count=?, reclaimable=?`）、调用点 `app_ops.go:267`。
- 危害：同一份导出文件，**修剪前导入**与**修剪后导入**得到两个不同的 `scanKey` ⇒ 去重判据失效，历史页多出一条"看起来是同一轮扫描"的行；再经 M356 的裁剪逻辑，多出的这条会**把本地最旧一条真实记录淘汰掉**。
- 覆盖：`grep -rn "Prune" internal/history/import*.go` **零命中** ⇒ 这条链路一条断言都没有。
- 取证命令：见上三条 sed/grep。

### P1-5 `replaySelection` 的回放只覆盖第一页 ⇒ 翻一次显示偏好会静默丢掉第 100 行之后的勾选

- 坐标：`frontend/src/utils/selection.ts:25-38`（判据：只保留 `groups` 里仍可见的 id）、`frontend/src/stores/scan.ts:345`（`selection.value = replaySelection(prevSel, groups.value)`）、`scan.ts:331-334`（非 append 分支 `groups.value = r.groups; resultPage.value = 1`）、`scan.ts:778-780`（`refreshPendingProjection` 用 `loadResultPage(false, true)`）、`scan.ts:61/65`（`pageSize = 100`、`DEFAULT_LOAD_CAP = 2000`）、`scan.ts:360-363`（`loadMore` 走 append 累积行集）。
- 形状：用户经 `loadMore` 累积到第 2~20 页并勾选 → 打开「隐藏非拟处理项」→ 非 append 重取**只回第 0 页（100 组）** → `groups.value` 被换成第 1 页 → 第 100 组之后的勾选被 `replaySelection` 判为"不可见"而丢掉。
- 这正是 **M342 要防的那类危害的残半**（M342 的注释 `selection.ts:8-11` 自称两条边缺一不成立；边①"仍可见且可勾的行一字不变"在**分页维度**上不成立），并且行集从最多 2000 组塌回 100 组是用户没有请求的。
- 覆盖：`grep -rn "resultPage\|loadMore\|append" frontend/tests/*.ts` **零命中** ⇒ 现有 6 条 `hide-nonpending-selection.test.ts` 全部只喂合成 `page(...)`，钉不到 store 层的分页交互。
- 取证命令：`cat frontend/src/utils/selection.ts`；`sed -n '325,350p;360,366p;770,782p' frontend/src/stores/scan.ts`；`grep -rn "append" frontend/tests/`。

## 四、P2 建议（承诺与实际不符 / 取证坐标失效，不产出错误数据）

### P2-1 导出/导入不进门禁、不进 `a.wg`（复核后**自 P1 降级**）

- 坐标：`app_records_io.go:103`、`:250`（两处只有 `busy := a.opsRunning`，未查 `maintaining`/`scanInFlight`）、落点在 `:213` `os.Rename(tmpDB, dbPath)` 与 `:217` `os.Rename(tmpJSON, jsonPath)`（`:218-222` 有回滚）、预检在 `:140-148`；`app_lifecycle.go:144` `if !a.opsRunning && !a.scanInFlight {` 决定是否等待。
- 实际后果（逐条读过才定级）：进程在导入中途被杀，留下的是一份**合法可导入的** `.db`，或一个会在下次尝试时自我拦下的 `.tmp` ⇒ 破的是"不留半份/有回执"的承诺，**不损坏用户数据** ⇒ 记 P2 不记 P1。

### P2-2 `app_settings.go:39` 的 `.corrupt` 改名会静默覆盖上一次的证据

- `os.Rename(path, path+".corrupt")` 目标已存在时直接替换。第二次配置损坏时，第一份损坏件（唯一能证明"为什么坏"的物证）消失。修法形状有分歧（加时间戳 / 加序号 / 保留不覆盖），**交裁定**。

### P2-3 「界面须显示"实占未统计"」是**死承诺**

- 三处代码注释把义务派给渲染层：`app.go:101-107`、`app_history.go:66-70`、`frontend/src/wails.ts:113-117`（"界面须显示'实占未统计'而不是这个数"）。
- 现读：`grep -rn "ActualKnown\|actualKnown" frontend/src frontend/tests` ⇒ 命中**只有类型声明**（`wails.ts:118`）与注释，**零个读取者**；`grep -rn "实占\|未统计" frontend/src` 零命中（除注释）。
- ⇒ 不是 bug（界面根本没显示实占数，所以没说谎），是**注释指派了一条不存在的防线**。同类：`docs/04:1832` 承认"当前界面仍只显示逻辑口径" —— 三处注释与该句必须收归一种说法。

### P2-4 取证坐标过期面**比在册的 M340 更宽**：M340 只盘了文档，Go 源注释一枚都没进

- 已登记面（不重报）：`docs/04` 登记表与四类文档里的 `app.go:NNN`，处置=登记不修，理由写在 §6.59 二·2（纪律保护的历史锚）。
- **未在册**（本条新报）：全仓 `*.go` 源注释里 **15 枚** `app.go:NNN`，而 M336 拆分后 `app.go` 里 `func (a *App)` 的个数是 **0**（`grep -c '^func (a \*App) [A-Z]' app.go` = 0）⇒ 15 枚**全部失效**：
  - 7 枚越界（`app.go` 现长 1148）：`internal/ops/executor.go:394`（`app.go:2095`）、`:409`（`app.go:1898-1901`）、`internal/ops/verify.go:206`（同）、`internal/ops/executor_dedup_fid_r4_test.go:8`（`app.go:1664`、`:2576`）、`app_undo_test.go:507`（`app.go:2326`）、`app_p0_p1_test.go:109`（`app.go:1805-1820`）；
  - 8 枚"行号可达但指向无关内容"：`internal/dedup/pipeline.go:242` 引 `app.go:591` 说是"scanInFlight 挡板"，现读该处是保留项选择循环（`scanInFlight` 字段实际在 `app.go:287`）；`app_preview_p3_test.go:120` 引 `app.go:27` 说是 `_ "image/gif"`，实际在 `app.go:35`；`app_hist_race_test.go:6` 引 `app.go:334` 说是 `a.hist.Close()`，实际在 `app_lifecycle.go:212`。
- 同一族的第二处（M337/M341 抽出 `pipeline_stages.go` 造成，`pipeline.go` 现长 699）：`internal/dedup/group_order_second_key_test.go:3/:16`（引 `:809-811`，三级排序实际在 `pipeline_stages.go:583-591`，被引的那一级"成员数降序"在 `:587-589`）、`group_order_probe_test.go:7/:13`（引 `:765/:791/:812`）、`pipeline_behavior_lock_test.go:5`（引 `:349-935`）、`internal/scanner/scanner.go:252` 与 `scanner_probe_ctx_test.go:8`（引 `pipeline.go:351`，取消分支实际在 `pipeline_stages.go:97-100`）、`internal/scanner/filekey_other.go:16`（引 `pipeline.go:438`，`ResolveKey` 实际在 `pipeline_stages.go:141`）。
- ★ 结构性原因（值得单独记）：门禁第 16 行的扫描面是 `find docs -name '*.md' | awk -f scripts/anchor-hits.awk docs/*.md` ⇒ **任何 `*.go` 里的坐标都不在任何尺子下面**，所以它不会随 M340 的 OOR 一起被报出来。

### P2-5 三处文档/脚本把 CI 坐标写死了行号，现已漂

- `docs/04:762`：版本同步断言写成"`build.yml` **首步**"。M242 已把它移到 Setup Go 之后 ⇒ 现读 `build.yml:146`，是 build job 的**第 8 步**（前面是 Checkout / Install Linux Dependencies / Setup Node / Build Frontend / Setup Go）。
- `docs/04:472-473` 与 `scripts/run-gates.sh:114-115`：两处都写"第 3/4 行与 `ci.yml:98`、`:101` 同一形状"。现读 `GOOS=windows GOARCH=amd64` 在 `ci.yml:100`、darwin 在 `:103` ⇒ 各差 2 行。
- ★ 修法取向：本仓已有规矩"引用改成锚点命令而不是数字"（§6.21 三立的），这三处属该规矩的漏网格。
- ⚠ `build.yml` 属 **D1 冻结面**（用户原话"这两个等我手动改完再说，别动"）⇒ 本条**只指出文档那句话过期**，不建议对 `build.yml` 做任何改动。

### P2-6 `docs/04` §2.3 的绑定方法数：锚和值同时失效

- 坐标：`docs/04:80` 写「**绑定方法 38 个**」，自带锚 `grep -c '^func (a \*App) [A-Z]' app.go`。现读：该锚输出 **0**（M336 拆分后 app.go 不含绑定方法），全 `app*.go` 现读 **40**（`grep -h '^func (a \*App) [A-Z]' app*.go | wc -l`）。
- ⇒ 该行**自述的重取通道已经读不到自己给的数**，属 M210「文档跟着代码走」管辖的活文档格，与 P2-4 那族"历史锚不许回改"不同类。

### P2-7 `safeHref` 是协议白名单过滤器，但零判据

- 坐标：`frontend/src/utils/markdown.ts:30-`（导出）、`:54`（唯一内部调用点）、`frontend/src/components/MarkdownView.vue:7`（注释声明"做协议白名单过滤"）。
- 真实在渲染路径上：`PreviewPanel.vue:15` import、`:136` `<MarkdownView v-else-if="isMd && rendered" ...>`。
- 负向断言（主代理自跑）：`grep -rln "markdown" frontend/tests/` **零命中**；`grep -rn "markdown" scripts/*.sh` **零命中** ⇒ 无单测、无接线锚。`docs/*.md` 里也没有它的承诺句。（`.workbuddy/` 下的三份同名文件是 gitignore 的历史副本，不构成覆盖。）

### P2-8 `internal/ops/trash.go:11` 的注释与 Windows 实现相反

- 注释："windows 无公开映射接口，恒为空 map"。现读：`trash_windows.go:288 fillRecycledDst`、`:323 scanRecycleBin`、`recycle_index.go:165` 都在实际建索引 ⇒ 注释描述的是旧实现。行为无影响（代码是对的），话术过期。

### P2-9 划账里 60 处"日志留档 `build/m7scratch/…`"指向的目录**从未入库**

- 现读：`grep -c "build/m7scratch" docs/04-开发与测试计划.md` = **60**；`git log -- build/m7scratch` **零提交**；`git status` 显示 `?? build/m7scratch/`（`.gitignore` 只挡 `build/bin`，没挡它 ⇒ 不是刻意忽略，是**一直没提交**）。
- 后果：历批写在划账里的"证据留档于 `build/m7scratch/xxx.log`"这类句子，**只在这台开发机上成立**——换机器、CI、第二会话克隆后全部读不到，而 `docs/04` 的文本形状与"证据在库里"无法区分。本轮基线（`build/m7scratch/audit9-baseline.log`）同样在目录外。
- ★ 取向不自行选边：① 纳入版本控制（会带来日志体积与噪音）；② 把留档句改成"读数已抄进正文，文件仅在本地"；③ 移到明确忽略的 `tmp/` 并把 60 处引用统一改口。三条都要动 60 格，属一次裁定。

## 五、复核后**不成立 / 不新**的条目（写明以防下一轮原样再报）

| 报法 | 复核结论 |
|---|---|
| `settings.json` 写成 0644 是缺陷 | **不新**：已在册且是明写的边界（设计段 §4 第 3 条；`docs/09:230` 边界② 点名）。 |
| `-wal` / `-shm` 写入期 0644 | **在册待裁**：M379（`docs/04` §6.69 八），两个取向待用户裁定，本批零代码改动是刻意的。 |
| "导出/导入会损坏用户数据库"（A/D 路一度按 P1 报） | **降级为 P2-1**：中途被杀留下的是合法可导入的 `.db` 或自拦的 `.tmp`，见 P2-1 的逐条读码。 |
| `app.go` 67 个方法 / 3150 行 职责过载 | **不新**：M336 已按六簇拆完（`docs/04:3387`），现读 1148 行。本轮回报的相关条目只剩"锚没跟着搬"（P2-4）。 |
| §6.26 五·2 那句"`build.yml` 里 `draft` grep 仍零命中" | **不构成漂移**：那是那一批的历史读数，且 `docs/04` 只增不删；`draft: true` 由 M223 在 `build.yml:286` 落地，历史行不回填。 |
| 子代理报告的 P0 若干 | 本轮六路**均未报 P0**，故无"整份作废"发生；所有负向断言由主代理自己跑 grep 后才入账（见 §二）。 |

## 六、覆盖面边界（★ 必读，本轮结论的适用半径）

**覆盖了**：上表六路的源码与判据；本轮新增/在册功能的面（M364/M365/M366/M375/M376/M377 的落地形状、M342 前端回放、M356 裁剪、M354 平台语义）。

**没有覆盖，因此"未发现 P0"不等于"没有 P0"**：

1. **真机面全缺**：Windows / Linux 真机行为零读数（`docs/05` 清单本机可测格本轮未执行）；`M31~M35`、`M89` 执行腿、`M120`（需 tag/dispatch）仍按历批口径挂账。
2. **CI 三条腿本轮零读数**：本轮没有推送，故没有任何 CI 侧新证据；上一批的 run 读数不代这一批说话。
3. **并发/时序面只做了静态复核**：P1-1 与 P2-1 的窗口是靠读码 + 既有 `warnLedger` 文案定位的，**没有构造出真红探针**（按本仓纪律，没探针就没资格说"已取证到触发"）。
4. **端到端与性能面未做**：无 CLI 全链路耗时、无 `dedup` 大图数据集、无 `race` 长跑（基线只跑了 `-count=1`，历批的 `-race` 结论不回填）。
5. **第 15 行 `smoke-symlink` 仍是 SKIP**（本机非 root，按 AS-K2 **不计通过**）。
6. **`docs/01/02/03` 与 `docs/archive/*` 未逐格审**：按"冻结档案只增不改"纪律，只核了其中被引用的坐标是否可达（结果见 P2-4/P2-5）。
7. 前端只审了 `src/stores` `src/views` `src/components` `src/utils` 的判据与接线，**未跑浏览器**（无 DOM 级验证）。

## 七、建议的处置顺序（取向仍交用户，本报告不选边）

| 组 | 条目 | 一句话 |
|---|---|---|
| 建议先裁 | **P1-1 / P1-2** | 同一族：门禁的外扩边界（回撤腿、第四枚破坏性控件）。裁一条"门禁覆盖清单"就能一起收。 |
| 建议先裁 | **P1-3 / P1-4** | 都动到"口径正确性"，且都需要一次修法取向选择（P1-3：改调用方还是改 `ReclaimActual` 契约；P1-4：`scanKey` 是否把可派生列剔出键）。 |
| 建议单独一批 | **P1-5** | 前端分页与回放，需要 store 级判据（现有 node 测试的通道读不到 `.vue`/pinia）。 |
| 可并批纯文档 | **P2-3 / P2-5 / P2-6 / P2-8** | 四处话术/坐标收归，零行为改动。 |
| 需一次裁定要不要做 | **P2-4** | 是否把 Go 源注释纳入一把新的锚点尺子（与 M340 的"纪律代价"边界直接相关）。 |
| 需一次裁定（牵动 60 格） | **P2-9** | `build/m7scratch/` 那 60 处"留档"引用是入库、改口、还是搬家。 |
| 待裁不动 | **M379**（两取向）、**M75(b)**、挂账 **M186** | 历批遗留，本轮不重复展开。 |

> D1（`build.yml` 加固）与 D3（CI 腿 `smoke-symlink` 判红）**仍按用户裁定未动、未建议改动面**。
