# 第九轮修订批细化设计段（2026-10-01 · 报告 → 裁定 → 落地）

取证基线：`HEAD = a7bf7bd`（报告笔），代码基线 `ccdea3d`。全部坐标按 `ccdea3d` 现读，落地后会漂的已在「八、边界」点名。

## 〇、本批定位与四条裁定原文

本批范围 = `docs/superpowers/specs/2026-10-01-full-audit9-report.md` 里被复核证实的 **P1×5 + P2×9**，加上用户 2026-10-01 通过 AskUserQuestion 下达的**四条裁定**。

| # | 待裁格 | 用户裁定（原文选项） | 本批落点 |
|---|---|---|---|
| 裁-1 | M379 写入期 `-wal`/`-shm` 仍 0644 | **写入腿各补收紧** | §4（C） |
| 裁-2 | M75(b) UPSERT 无条件覆盖 × 失效判据含 ctime | **失效判据剔除 ctime** | §3.3（B3） |
| 裁-3 | P1-4 `PruneScanFiles` 重写 `scanKey` 三列 | **键剔除三列派生值** | §3.2（B2） |
| 裁-4 | P2-9 划账 60 处留档引用指向未入库目录 | **改口：读数在正文里** | §6.7（E7） |

纪律照旧：**拟号 ≠ 分配**，分配在 `docs/04` 号表（本段只给拟号）；修前必红探针 + 变异对账 + 全套门禁读数逐条入划账；**不为让门禁变绿而改既有断言**——本批确实要动两条既有断言（§3.2、§3.3），两处都是**裁定直接改写了契约**，逐字写明裁-3／裁-2 原文与改前改后期望，不冒充"测试本来就红"。

## 一、现读取证（本批全部改动面的地基）

1. `grep -n "maintaining" app*.go`（非测试）＝ `app.go:291/300/383/405/406/408/415`、`app_ops.go:55/56`、`app_scan.go:59/60` —— **两个 Undo 函数体内零命中**（入口 `app_ops.go:291-305`、`:403-420`，锁内只问 `opsRunning`/`scanInFlight`）。
2. `app_history.go:119-148`：`DeleteScanHistory` / `ClearScanHistory` 体内**无任何门禁**（该文件的门只在 `:52-55`、`:93-96`、`claimMaintenance` 那两条腿）。对照样板 `ClearOpRecords`（`:323-330`）：`claimMaintenance("清空清理记录","清空记录")` + `defer releaseMaintenance`。
3. 前端：`RecordsView.vue:287` 与 `:289` 两枚按钮**无 `:disabled`**（同文件 `:298`/`:302` 的 ops 腿吃 `store.busy || clearingOps`；`:354` 的行内删除吃 `store.histBusy`）；`RecordsView.vue:50-54` `clearAll()` 把 `confirmClear` 复位放在 `await` **之前**；`scan.ts:475-483 clearHistory()` 与 `:465-473 deleteHistory()` 都不走 `guard()`（对照 `:534 clearOps()`）。
4. `internal/model/model.go:152-177`：`actualKnownIn` 与 `ReclaimActual` 都从 `files[1:]` 起算，注释明写"约定 files[0] 为保留项"；而 `internal/ops/keep.go:97-112` 的 `pickShortest`（经 `SuggestKeepIndex` 对外，`:79-86`）是"先非隐藏、再路径最短"，`internal/dedup/pipeline.go:612-614` 的 `sortEntries` 只按 `Path <` 字典序 ⇒ **默认保留项通常不是 index 0**。
5. `grep -rn "ReclaimActual\|AnyActualKnown"` 生产者两处：`pipeline_stages.go:575`（建组时存 `ReclaimableActual`）、`app_ops.go:256`（裁剪后重算）；标志位生产者 `app.go:585`、`cmd/fdd-cli/main.go:176`。消费者只读**存量列**，不重算。
6. `internal/history/import.go:206-218` 键九字段、`:410-413` 构造、`:473-500` 从库里 SELECT 十列回填键；`internal/history/scan.go:363-367` 的 `PruneScanFiles` 在一条事务里重写 `groups_count / files_count / reclaimable`。
7. `internal/cache/cache.go:391`：`if id.Resolved && (e.Dev != id.Dev || e.Ino != id.Ino || e.CtimeNs != id.CtimeNs)` —— ctime 是失效判据的一腿；`:446` UPSERT 无条件写回 `dev/ino/ctime_ns`。既有断言 `cache_test.go:198-224`（`TestLookupIdentityMismatch`）第三格 `{CtimeNs: 89}` 期望**未命中**。
8. 档位：`cache.go:184` / `history.go:183` 各一枚 `hardenFileMode` 接缝，只在 `Open`（`cache.go:220`、`history.go:214`）里用一次；写入腿（`Store`、`reclaimLocked` 的 `checkpointFn`，`cache.go:612-618`）都没碰伴随文件。
9. 前端分页：`scan.ts:61 pageSize=100`、`:65 DEFAULT_LOAD_CAP=2000`、`:360-363 loadMore` 走 append 累积、`:331-334` 非 append 分支 `groups.value = r.groups; resultPage.value = 1`、`:778-780` 显示偏好重取用 `loadResultPage(false, true)` ⇒ 已累积行集被换成第 0 页。
10. `frontend/src/utils/markdown.ts:30-37` `safeHref`：白名单为「`#/?` 开头 ‖ `^[\w.\-]+/` ‖ `^(https?:|mailto:)`」，其余返回 null。渲染路径真实存在（`PreviewPanel.vue:15/:136`）。`grep -rln markdown frontend/tests` 与 `grep -rn markdown scripts/*.sh` **均零命中**。
11. `app_settings.go:39` `os.Rename(path, path+".corrupt")` —— 目标已存在即覆盖，无时间戳分叉。
12. `app_records_io.go:103` / `:250` 两处只 `busy := a.opsRunning`；`app_lifecycle.go:144` 的关窗条件 `!a.opsRunning && !a.scanInFlight` 不含维护闩。
13. 坐标可达性：`wc -l app.go` = **1148**、`grep -c '^func (a \*App) [A-Z]' app.go` = **0**、全 `app*.go` = **40**；`wc -l internal/dedup/pipeline.go` = **699**。

## 二、实施 A：把维护闩的覆盖面补齐（P1-1 / P1-2）

### A1　回撤两条腿接 `maintaining`（拟 M380）

- 改法：`UndoOperation`（`app_ops.go:291`）与 `UndoOperationItem`（`:403`）在**同一个 `a.mu` 临界区**里，于 `opsRunning`/`scanInFlight` 之后补第三问：
  ```go
  if a.maintaining != "" {
      a.mu.Unlock()
      return "", fmt.Errorf("%s进行中，请等待完成后再回撤", a.maintaining)
  }
  ```
  话术与 `claimMaintenance` 第三分支逐字同形（点名在等什么，不造无主的"维护进行中"）。
- 同步更正话术：`app.go:377-378` 那句"双向闭合：维护先占住则扫描/清理被拒"改成**点名覆盖清单**（扫描 / 清理 / 回撤 / 载入历史 / 导出导入），并把"清单本身是判据"写进注释——不对称就是这个 bug 的成因。
- 判据（**P-44**，Go 根包）：`ClearOpRecords` 在接缝里暂停（复用既有 `ledgerClearOps` 接缝），期间发起 `UndoOperation` 必须被拒且**一次文件操作都没发生**；负控制：无维护时回撤照常受理。改前必红：撤掉第三问 ⇒ 回撤被受理 ⇒ 断言当场红。
- 变异 **MU9-a**：删掉 `UndoOperation` 那一问 ⇒ 必须红在 P-44（不是红在别的用例）。变异 **MU9-b**：把问挪到锁外（先问后占的两段式）⇒ P-44 仍须红（判据是"同一个临界区"，不是"问过就行"）。

### A2　清空/删除扫描历史补齐四层（拟 M381）

后端：`DeleteScanHistory` 走 `claimMaintenance("删除历史记录","删除历史记录")` + `defer releaseMaintenance`；`ClearScanHistory` 走 `claimMaintenance("清空全部历史","清空全部历史")`。两者都在**占位期间**做 SQL，与 `ClearOpRecords` 同形（闩不是锁，库操作在 `a.mu` 之外）。
- 判据（**P-45**）：扫描写回在途（`scanInFlight`）时 `ClearScanHistory` 必须被拒；清理/回撤在途（`opsRunning`）时 `DeleteScanHistory` 必须被拒；两项维护互相排斥（导出在途时清空必须被拒，见 §6 C 批的 `a.maintaining` 覆盖面）。
- 判据（**P-46** 负控制，改前即绿）：空闲时清空/删除照常成功，`curHistID` 联动按既有语义断开。

前端三层（同一批，缺一层就是"点了才报错"或"能点两下"）：
1. `scan.ts clearHistory()` / `deleteHistory()` 首行 `if (!guard('清空全部历史')) return` / `guard('删除历史记录')`；
2. `RecordsView.vue` 新增 `clearingHistory = ref(false)`，`:287`/`:289` 两枚按钮补 `:disabled="store.busy || clearingHistory"`（与 ops 腿 `:298`/`:302` 逐字同形）；
3. `clearAll()` 的 `confirmClear` 复位移到 `finally`，`await` 期间按钮由 `clearingHistory` 拦 ⇒ 双击打不出两次后端调用。
- 判据（**P-47**，接线锚，`scripts/test-frontend-logic.sh` 的静态锚通道）：`.vue` 打不进 `node --test`（M116/M118 同族限制），故钉"两枚按钮的 `:disabled` 表达式含 `clearingHistory`" + "`clearAll` 里 `confirmClear` 的复位在 `finally` 内" + "`clearHistory()` 体内出现 `guard(`"三格。**不得**称行为级红；行为半边由 P-45/P-46 的后端判据兜住。

## 三、实施 B：三处口径收归（P1-3 / 裁-3 / 裁-2）

### B1　实占可释放量按**默认保留项**扣（拟 M382，残半另立 M388）

- 收归形状：把 `pickShortest` 的规则**上收一份到 `internal/model`**（新增 `PickDefaultKeepIndex(files []*FileEntry) int`：先非隐藏、再路径最短，无成员返回 -1），`internal/ops/keep.go` 的 `pickShortest` 改为委托它（**取值与顺序一字不变**，I5"同一判据一份实现"，dedup 与 ops 从此用同一把尺子）。
- `ReclaimActual(files)` / `actualKnownIn(files)` 的下标约定从"跳过 index 0"改为"跳过 `PickDefaultKeepIndex(files)`"；**签名不变**（三个生产者传的都是整组 `g.Files`，语义就是"应用默认策略后能释放多少"）。
- 判据（**P-48**，model 包）：造一条三员组，字典序最小者**不是**默认保留项（例如 `a/x` 最短但 `b/x` 非隐藏更短……实际用"隐藏目录使 index 0 不是保留项"的形状），三条成员实占互不相等 ⇒ 断 `ReclaimActual` 扣的是默认保留项那一条；改前必红（扣的是 `files[0]`）。
- 判据（**P-49** 负控制）：默认保留项恰在 index 0 时，改后与改前逐字节同值（防"修一个口径顺手改坏另一个"）。
- 判据（**P-50**）：`AnyActualKnown` 同一格——保留项自己读到过实占、冗余项全 unknown ⇒ 标志必须为 **false**（MODEL-1 的原意就是这个，只是原先靠 `files[1:]` 表达）。
- **残半登记（拟 M388）**：用户手动改选保留项后，存量列 `ReclaimableActual` 仍按默认保留项 ⇒ 与实际清掉的那份差一条实占差值。修法要动视图层重算或改存列语义，本批不做，登记待裁。

### B2　`scanKey` 剔掉三列派生值（裁-3；拟 M383，并更正 M377 的方向句）

- 键改为六字段：`SavedAt / Roots / Filters / Threads / Paranoid / OrigFiles`；`GroupsCount / FilesCount / Reclaimable` 出键（它们会被 `PruneScanFiles` 在事务里重写，是**派生值**，进键就等于"同一份判断被修剪过 ⇒ 判成新扫描"）。
- 同步：`:410-413` 构造点、`:473-500` 的 SELECT 收窄（不再读那三列）、`localScanKeys` 的注释。
- 判据（**P-51**）：导出→导入→对本地那条做一次 `PruneScanFiles`→**再导入同一份影像** ⇒ 第二次必须 `ScansSkipped++`、五表一字不动。改前必红（键含三列 ⇒ 认成新行并触发裁剪淘汰最旧）。
- 判据（**P-52** 负控制）：`Threads` 与 `OrigFiles` 各单列不同 ⇒ 仍认成新行（M377 立的那半边不许被本批顺手拆掉）。
- **既有断言的契约变更（如实记账）**：`import_m377_test.go:144-174`（P-40）的 `{"reclaimable", …}` 子档，改前期望"单列不同 ⇒ 成新行"，按裁-3 必须翻成"单列不同 ⇒ 仍判重复"。本批把该子档**移出** P-40 并改写为 P-51 的相邻格（在同文件写清"2026-10-01 裁-3 把可回收量判定为派生值，故此档期望反转"），不是把它删掉、也不是为了让门禁绿。
- 手册双回写：`docs/09:792-793`、`docs/10:559` 的"九字段"说法按 M210 就地更新为六字段，旧句不删、加日期化方括号追记，并明写"与 M377 的方向并不矛盾：M377 补的是**不可派生**的工况与读数，本批剔的是**会被重写**的三列"。

### B3　缓存失效判据剔除 ctime（裁-2 = 原 M75(b) 销号）

- 改法：`cache.go:391` 条件收成 `if id.Resolved && (e.Dev != id.Dev || e.Ino != id.Ino)`；`ctime_ns` 列**继续写入**（留证与将来复盘用），只是不再参与失效判定。H1 那段注释同步改写：mtime/size 一道、`(dev,ino)` 一道，"原地改写保 mtime 且保 inode"那一档明确交棒给** paranoid 逐字节 + 破坏性操作前的全量哈希复核**（`docs/09` §6.1 第 3 条），不再冒充 ctime 兜住。
- 一致性收归：这条与 `fsid.SameIdentity` **刻意不含 ctime** 的旧裁定（DOC-H2）对齐；`docs/09` 若有"元数据推进即失效"口径的句子一并更正。
- **既有断言的契约变更（如实记账）**：`cache_test.go:198-224` 第三格 `{CtimeNs: 89}` 从"必须未命中"改为"**必须命中**"，并在同处补一句正向断言（mtime/size/dev/ino 同、仅 ctime 推进 ⇒ 命中）。这不是放松测试：裁-2 就是把契约改了，测试必须跟着新契约，且**反向**由变异证明判据仍在。
- 判据（**P-53**）：dev 变 / ino 变 两格仍必须未命中（防"剔 ctime 顺手把身份也剔了"）。
- 判据（**P-54** 负控制）：`fsid_ctime_m271_test.go` 那条 Windows ctime 读数判据**一字不动**——它读的是"字段有没有读错"，不是"要不要参与失效"，本批不许借机降级它。
- 变异 **MU9-c**：把整条 `id.Resolved && …` 判据删掉 ⇒ 必须红在 P-53（不是红在 P-53 以外）。

## 四、实施 C：M379 写入腿各补收紧（裁-1；沿用既有号，不新占）

- 新增接缝（每包一枚，跨包不耦合，惯例同 `hardenFileMode`）：
  ```go
  var hardenSidecarMode = func(path string) error { return os.Chmod(path, 0o600) }
  func hardenSidecars(base string) // 对 base+"-wal" / base+"-shm" 各来一次；os.ErrNotExist 静默跳过
  ```
- 调用点：① `Open` 里 `hardenFileMode(path)` 之后（覆盖上次异常退出留下的伴随文件）；② `Store` 提交之后；③ `reclaimLocked` 的 `checkpointFn` 之后。history 侧同形（它的写腿在 `SaveScan` / `FinalizeOp` 收口处，实施时按现读取第一个"事务已提交"的点）。
- 失败口径沿用 M376 的裁定：**只出声、不升级成可用性事故**（stderr 一行带系统原文），因为 `-shm` 在某些时刻本就不存在是常态。
- 判据（**P-55**，darwin/linux 腿）：400 条 `Store` 在飞时 `Stat` 三个文件 ⇒ 主库与 `-wal` 都必须是 `0600`。改前必红（`-wal` 读回 `0644`，正是 §6.69 八那次一次性探针的实测读数）。
- 判据（**P-56** 负控制）：全部 `Close` 后 `-wal`/`-shm` 已由 SQLite 删除 ⇒ `hardenSidecars` 不得因"文件不存在"报错出声。
- 边界（写进划账）：Windows 腿模式位读回恒 `0666`（M354），本条只在 unix 兑现；`-wal` 的**创建时刻**仍是 SQLite 按 umask 落的 0644，收紧发生在提交之后 ⇒ 从"SQLite 建文件"到"我们补档"之间的窗口只能缩小、不能归零（除非收目录 0700，那是裁-1 明确没选的取向）。

## 五、实施 D：前端两格（P1-5 / P2-7）

### D1　显示偏好重取不塌行集（拟 M386）

> ★ **实施期更正（本段原文有错，按现读代码改写；下面 D1 的改法与判据以本更正为准）**
>
> 1. **"一次请求取回已加载的那一段"取不到**：后端 `app_result.go:34-38` 把 `PageSize` 钳到
>    **500**（`> 500` 时钳到上限，M10 的溢出钉子就钉在这个值上），而 `DEFAULT_LOAD_CAP` 初始就是
>    2000（`scan.ts:65`）。若照本段原写"size=loaded、resultPage=ceil(loaded/100)"，800 组那一档
>    实际只回 500 行却把 `resultPage` 写成 8 ⇒ 下一次追加从偏移 800 起，**第 500~799 组被永久跳过**
>    ——修法本身比原缺陷更会丢数据。因此改法换成**分段取回**：每段 `pageSize` 不超过后端上限，
>    段数 = `ceil(loaded / 段大小)`，取回后截到 `loaded` 行，再写回 `resultPage = ceil(loaded / 100)`。
> 2. **"pinia store 打不进 node --test"这句是错的**（本段 §八.3 同错）：`tests/harness.mjs` 自 M16
>    起就能在 node 里实例化真 store（`tests/scan-resultset.test.ts` 就是活例，它断言的正是
>    `doLoadResultPage` 发出的查询参数）。所以 P-58 从"静态接线锚"**升级为 store 级行为探针**，
>    真能跑到"累积 800 行 → 翻开关 → 行集与勾选"这一格。`utils/selection.ts:3-5` 那句同源过期话术
>    一并更正（M210：文档跟着代码走）。
>
> 更正后的判据（标号不变，读数在划账笔现取）：
>
> - **P-57**（纯函数）：`projectionReload(800, 100)` ⇒ `{segmentSize:500, segments:2, nextPage:8}`；
>   `projectionReload(50, 100)` ⇒ `{100, 1, 1}`（原样，负控制）；`projectionReload(0, 100)` ⇒ 同上；
>   `projectionReload(2000, 100)` ⇒ `{500, 4, 20}`；再加两条**算术不变量**格：
>   `segments × segmentSize ≥ loaded`（取不回已加载段就是错）与 `nextPage × pageSize ≥ loaded`
>   （追加腿不得跳过已加载段之外的行）。★ 后一条正是原设计漏掉的那格。
> - **P-58**（行为探针，非静态锚）：桩具造 800 组分页数据 → `loadMore` 累积到 800 → 勾第 150 行后的
>   项 → 翻「隐藏非拟处理项」⇒ 断言 `groups.length` 仍为 800 **且**勾选仍在。改前必红（塌回 100 行、
>   勾选被 `replaySelection` 判为不可见而丢）。
> - **P-58b**（负控制）：同一桩具下 `append` 腿与不带 `keepSelection` 的重取**一字不变**（仍发
>   `page=…, pageSize=100`），防止把"分段"泄漏到放量路径上。

- 抽纯函数进 `frontend/src/utils/pageload.ts`（可被 `node --test` 导入，形状同 `utils/selection.ts`）：
  ```ts
  export const MAX_PAGE_SIZE = 500 // 与 app_result.go:36-38 的钳位值同值，两处必须一起改
  export interface ReloadPlan { segmentSize: number; segments: number; nextPage: number }
  export function projectionReload(loaded: number, pageSize: number, maxPageSize = MAX_PAGE_SIZE): ReloadPlan
  // loaded<=pageSize ⇒ {pageSize, 1, 1}                       // 一页都不到，行为与改前同形
  // 否则           ⇒ segmentSize=min(loaded,maxPageSize)
  //                 segments=ceil(loaded/segmentSize)         // 分段取，每段不触后端钳位
  //                 nextPage =ceil(loaded/pageSize)           // 写回 resultPage 的页号
  ```
  `doLoadResultPage` 在 `keepSelection && !append` 分支里用它决定请求段数与 `pageSize`，逐段
  `await`（每段之间复查 `resultGen`，换代即整趟作废）、按 `page=i, pageSize=segmentSize` 取，
  拼起来截到 `loaded` 行，并回写 `resultPage = nextPage`；任一段回包不满 `segmentSize` 就停止后续段
  （后端已到底）。append 腿与 `loadMore` 一字不动。
- 残余风险如实写：① 分段之间结果视图可能被并发换代（`viewCache` 的 guard 变化后重新排序），
  段边界上的行可能重复或缺一——这与**改前的追加腿本来就有**的暴露面同一族（`resultPage` 记账数的是
  页而不是行），本批不扩大处理，只在注释点名；② 一次重取从 1 次 IPC 变成最多 4 次（2000/500），
  首屏抖动与后端单次查询时间的代价由"分段"承担而不是放大；③ `loaded` 非 `pageSize` 整数倍只可能
  出现在"结果集已到底"之后（追加腿每次 +100），那时没有后续行可跳，`nextPage` 的向上取整无害。

### D2　`safeHref` 补白名单判据（拟 M387，观察项拟 M389）

- 新增 `frontend/tests/markdown-safehref.test.ts`（`markdown.ts` 无 vue/pinia 依赖，可直接被 node 导入）：逐格钉 `javascript:` / `data:` / `vbscript:` 返回 null；`http:` / `https:` / `mailto:` / `#x` / `/abs` / `rel/path` 原样放行；前后空白先 trim。
- ★ 现读到一个**必须记账但不擅自改**的形状：`/^[#/?]/` 会把协议相对 URL `//evil.example/x` 当"相对链接"放行（浏览器按当前协议解析成外站绝对地址）。预览内容来自用户磁盘上的不可信文件 ⇒ 要不要连协议相对一起拒，是取向问题：本批**先按现行为钉成断言**（M389 登记待裁），不擅自收紧，免得把"链接能不能点"这件事改成代理决定的产品行为。
- 变异 **MU9-d**：把 `safeHref` 改成恒等返回 ⇒ P-57…P-58 不管，必须红在 markdown 那三条里的至少一条；若一条都不红，说明用例是摆设，重做。

## 六、实施 E：话术与取证坐标收归（P2 文档面，零行为改动）

| 项 | 拟号 | 改法（现读坐标 → 新坐标） |
|---|---|---|
| E1 实占"界面须显示未统计"死承诺 | M390 | `app.go:101-107`、`app_history.go:66-70`、`frontend/src/wails.ts:113-117` 三处"界面须…"改成**明写现状**：界面当前不渲染实占口径（`docs/04:1832` 的"仍只显示逻辑口径"是唯一真话），`actualKnown` 是**留给 M8 的接线位**，不是已生效的防线。 |
| E2 Go 源注释里的失效坐标 | M391 | 15 枚 `app.go:NNN`（7 越界 + 8 指向无关内容）逐枚改指 `app_*.go` 现读坐标；6 枚 `pipeline.go:NNN` 改指 `pipeline_stages.go` 或 `pipeline.go` 现读行（取消分支 `pipeline_stages.go:97-100`、`ResolveKey` `:141`、三级排序 `:583-591`）。★ 顺带在 `docs/04` §6.30 类位置立一条读法：**第 16 行的扫描面是 `docs/*.md`，`*.go` 里的坐标不在任何尺子下**，按锚复核源注释必须自己开一眼。 |
| E3 §2.3 绑定方法数 | M392 | `docs/04:80`：值 38→**40**，锚从 `grep -c … app.go` 改成 `grep -h … app*.go \| wc -l`（旧锚现读 0，锚本身失效）。 |
| E4 硬编码行号三处 | M393 | `docs/04:472-473` 与 `scripts/run-gates.sh:114-115` 的 `ci.yml:98/:101` → 现读 `:100/:103`，并**改成锚点命令**（`grep -n 'GOOS=windows GOARCH=amd64 go vet'`）；`docs/04:762` 的"`build.yml` 首步" → "Setup Go 之后、只在 linux 腿跑一次的那一步"（引 M242，不复述行号）。⚠ **`build.yml` 本体属 D1 冻结面，一个字节都不动。** |
| E5 `docs/09:624` 两枚 ops 坐标 | M394 | `move.go:72`→`:110`、`symlink.go:117`→`:135`（`undo.go` 那枚已由 2026-09-23 追记更正为 `:225`）；10 手册若派生同一句则同步双回写。 |
| E6 `trash.go:11` Windows 注释 | M395 | "windows 无公开映射接口，恒为空 map" → 按 `trash_windows.go:288/:323` 与 `recycle_index.go:165` 的现实现改写。 |
| E7 留档改口（裁-4） | M396 | 在 `docs/04` 号表后加一段**全局读法追记**：`build/m7scratch/*.log` 从未入库（`.gitignore` 只挡 `build/bin`），历批"日志留档于…"一律读作"**读数已抄进本节正文，文件仅存在于当时的开发机**"；60 处旧句**不逐条改写**（按裁-4 的"改口"取向，用一条总追记覆盖，避免 60 格历史正文被动过）。此后新写划账不许再产出"留档于 `build/m7scratch/…`"式的证据句。 |
| E8 `.corrupt` 被静默覆盖 | M397 | `app_settings.go:39`：目标已存在时改落 `settings.json.corrupt-<unix 秒>`（撞名再 +1），并把最终落点写进 stderr 那句——与 M285（缓存隔离件点名落点）同一形状。★ 这是本批 E 组唯一的行为改动，方向选"不丢证据"而不是"覆盖最新"，理由：损坏件是唯一能解释"为什么坏"的物证，而它被第二次损坏抹掉是**加固动作自己销毁了证据**。 |

## 七、探针 / 变异汇总表（本批一次性列全）

| 标号 | 面 | 改前必须红的那一格 |
|---|---|---|
| P-44 | A1 回撤腿维护闩 | 维护在途时 `UndoOperation` 被拒且零文件动作 |
| P-45 | A2 后端门禁 | `scanInFlight`/`opsRunning` 在途时清空/删除历史被拒 |
| P-46 | A2 负控制 | 空闲时清空/删除照常（防判重判死） |
| P-47 | A2 接线锚 | 两枚按钮 `:disabled` 含 `clearingHistory`、`confirmClear` 复位在 `finally`、`clearHistory()` 走 `guard(` |
| P-48 | B1 实占口径 | 默认保留项不在 index 0 时扣的是保留项 |
| P-49 | B1 负控制 | 保留项在 index 0 时新旧同值 |
| P-50 | B1 标志位 | 保留项 known、冗余项全 unknown ⇒ 标志 false |
| P-51 | B2 键剔派生 | 修剪后再导入同一影像 ⇒ 仍判重复 |
| P-52 | B2 负控制 | `Threads` / `OrigFiles` 单列不同 ⇒ 仍成新行 |
| P-53 | B3 身份两腿 | dev 变 / ino 变仍必须未命中 |
| P-54 | B3 边界 | `fsid_ctime_m271_test.go` 一字不动（前提自检） |
| P-55 | C M379 | 400 条 Store 在飞时 `-wal` 读回 0600 |
| P-56 | C 负控制 | 关闭后伴随文件已不存在 ⇒ 不出声不报错 |
| P-57 | D1 纯函数 | `projectionReload(800,100) = {500, 2, 8}`（分段计划，含两条算术不变量格） |
| P-58 | D1 行为探针（store） | 累积 800 行后翻显示偏好 ⇒ 行集不塌、第 100 行后的勾选仍在 |
| P-58b | D1 负控制 | append 腿与普通重取仍发 `pageSize=100`（分段不得泄漏到放量路径） |
| P-59 | D2 白名单 | `javascript:`/`data:`/`vbscript:` ⇒ null（六格逐条） |
| MU9-a | A1 | 删回撤那一问 ⇒ 红在 P-44 |
| MU9-b | A1 | 把问挪到锁外 ⇒ 红在 P-44 |
| MU9-c | B3 | 删整条身份判据 ⇒ 红在 P-53 |
| MU9-d | D2 | `safeHref` 改恒等 ⇒ 红在 P-59 |
| MU9-e | B1 | `ReclaimActual` 改回 `files[1:]` ⇒ 红在 P-48 |
| MU9-f | C | `hardenSidecars` 改空函数 ⇒ 红在 P-55 |

★ 变异标号一律带 `MU9-` 前缀（§6.26 六的撞号规矩：裸号会与已登记条目 M26/M27/M28 那一族撞车）。每条变异跑完必须把**预测包名集合**与**实测包名集合**相减列出差集（§16.6 的口径：变异的价值在对账不在杀数）。

## 八、边界、未兑现面与坐标自毁声明

1. **本批改 `app.go`/`app_ops.go` 的行数会把 §一 里所有 `app.go:NNN` 推走**——本段与报告写的坐标一律标注为"`ccdea3d` 现读"，划账笔必须现取复跑，不许照抄。
2. **Windows 真机面零读数**：A2 的后端门禁在 windows 腿同样生效（纯 Go 逻辑），但 P-55（伴随文件档位）在 Windows 上按 M354 只能读回 0666 ⇒ **不得**写"三平台已验"；CI 三条腿只给编译与 vet 证据。
3. **P-47 是接线锚不是行为探针**：`.vue` 组件打不进 `node --test`（M116/M118 同族限制），措辞只能是"静态锚"。★ **但这一条不适用于 pinia store**：`tests/harness.mjs`（M16）能在 node 里实例化真 store，所以本批的 D1 判据按更正走**行为探针**（见 §五 D1 的实施期更正），原写"只能静态锚"那句作废。
4. **B1 只修默认策略那一档**：手动改选保留项后的分叉登记为 M388，本批不修 ⇒ 对用户说"实占口径已一致"不成立，成立的说法是"默认策略路径已一致，手动覆盖仍有已知分叉"。
5. **`-wal` 创建瞬间仍是 0644**：窗口只能缩小（裁-1 未选目录级取向）。这句必须进 09/10 的边界句，防止手册读成"写入期也仅所有者可读"。
6. **协议相对 URL 放行**是现行为、不是已裁定（M389）。
7. **CI 只在收口笔之后才有读数**；本段与实施笔不产出任何 CI 证据，划账里"CI 已绿"一类句子必须等推送那一笔现取。
8. `docs/04` §3.2 逐包计数在实施笔落盘后**必然红一次行 12**（新增测试文件），按既有规矩：实施笔落盘之后、划账笔之前先跑全套，红了改文档、再重取行 12/16。

## 九、拟号清单（★ 未分配；分配在 `docs/04` 号表，见划账笔）

M380 回撤腿维护闩 ｜ M381 清空/删除历史四层 ｜ M382 实占求和口径 ｜ M383 `scanKey` 剔派生列 ｜ M384 手动保留项与实占快照分叉（登记不修）｜ M385 `safeHref` 零判据 ｜ M386 显示偏好重取塌行集 ｜ M387 协议相对 URL 放行（待裁）｜ M388 实占"未统计"死承诺 ｜ M389 Go 源注释坐标无尺子 ｜ M390 绑定方法锚自指失效 ｜ M391 硬编码 CI 行号 ｜ M392 09 ops 坐标过期 ｜ M393 `trash.go` Windows 注释过期 ｜ M394 留档引用未入库（裁定=总追记改口）｜ M395 `.corrupt` 覆盖 ｜ M396 导出导入不接维护闩。

★ 上一段写的编号与实际占用可能有出入：按"拟号 ≠ 分配"，本段只声明**拟用区间 M380~M396**，最终以划账笔的号表为准；若复核时发现某格不成立（代码读下来不是缺陷），该号空置不复用，历批规矩照旧。

另：裁-1 落在**已有的 M379** 行上（销号），裁-2 落在**已有的 M75(b)** 上（追记销号），两者**不占新号**。
