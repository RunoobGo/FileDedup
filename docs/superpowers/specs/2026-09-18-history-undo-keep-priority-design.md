# 设计文档：保留策略多目录优先级 / 组内全选 / 扫描历史 / 清理记录与回撤

> **历史设计存档（2026-09-19 标注）**：本文冻结于 2026-09-18，功能已按此交付，但 0.5.0 后的
> 两轮审查（2026-09-18/19）改动了其中若干条。**不要据本文实现新工作或判断现行行为**，
> 现行口径看 `docs/09-用户手册.md` §6.6/§6.7 与 `docs/04-开发与测试计划.md`。
> 已知与本文不符处（详情见 `docs/archive/代码审查与修复编年史.md` 第 5 篇）：
>
> | 本文 | 现行实现 |
> |---|---|
> | §7.1 时序「账本全部失败只降级记账、不阻断清理本身」 | **已反转**（C3）：承诺可回撤的类型在 `BeginOp` 失败时**拒绝执行**（`app.go:1216`）；永久删除与 Windows 回收站（本就不可应用内回撤）留痕放行 |
> | §7.1 收尾统一 `Finalize` | 补 C6：`OnItem` 逐项即时落账，panic/取消不残留 `planned` |
> | §7.1 darwin trash「按行分帧 + 数量不等即放弃映射」（§8 同） | 改为脚本内输出 `src␟dst`（0x1F）成对行后配对（`internal/ops/trash_darwin.go:110`）；原方案在奇异引用与改名改基名下会丢配对 |
> | §9「不做单条 item 级回撤按钮」 | **已反转**：`UndoOperationItem` 已交付，明细行可逐文件回撤/失败重试 |
> | §10 回归基线「history/ops 新包 `-count=4`」 | 现门禁为全包 `-race -count=2` + **根包** `-count=4`（`ci.yml`） |
> | §11 版本号「三处口径」 | 现为 **5 处声明位**，由 `scripts/check-version-sync.sh` 与 tag 一致性门禁强制 |

- 日期：2026-09-18
- 版本目标：0.4.0 → 0.5.0
- 状态：已与需求方逐条澄清确认
- 存储决策：方案 A —— 独立 SQLite `history.db`（与哈希缓存 cache.db 物理隔离）

## 0. 需求与已确认语义

| # | 需求 | 已确认语义 |
|---|------|-----------|
| 1 | 保留策略多目录 + 优先级排序 | 可选多个保留目录并排序；组内文件命中多个保留目录时，保留优先级最高目录内那份；全部不命中时该组不产生决策（维持现状，与现有 directory 策略跳过语义一致） |
| 2 | 单项重复文件全选 | 每个重复组卡片头部加全选复选框，勾选/取消该组全部**非保留项**（保留项 isKeep 照旧不可选，S2 保护不变） |
| 3 | 扫描历史 | 自动保存历次扫描（上限 N=20 条，超出淘汰最旧）；历史面板可浏览、恢复任意一条为当前结果集继续清理、一键用当时配置重扫、删除单条/清空。恢复的旧结果依赖既有「操作前逐文件校验」兜底（文件已变→Failed，已消失→Skipped），不做打开时全量校验 |
| 4 | 清理记录 + 回撤 | 每次清理落操作日志（写前日志）；trash/move/hardlink 支持应用内回撤，delete 与 Windows 回收站操作标注「不可回撤/请到系统回收站还原」；回撤前逐项校验，宁可少撤不可错撤，绝不覆盖任何现有文件 |

## 1. 现状关键事实（设计依据）

- 保留策略：`model.KeepPolicy{Kind, Directory}` 单目录（internal/model/model.go:101）；`ops.ApplyKeepPolicy`/`pickInDirectory` 单目录最长前缀匹配（internal/ops/keep.go:91-105）。
- 勾选：前端全局 `selection: Set<fileID>`（stores/scan.ts:75），`selectAll` 为跨全部已加载组；GroupCard.vue 组内无全选。
- 结果集纯内存：`App.groups/byID/keepIDs`，`StartScan` 开始即清空（app.go:301-305）；`ExecuteOperation` 收尾后就地裁剪组（app.go:901-925）。清理后新扫描/重启即丢上下文。
- `ExecuteOperation` 入口要求 `pipe.Status()==Done`（app.go:852）——历史恢复场景下引擎状态是 Idle，此门槛需改造（见 §3.4）。
- 回收站三平台：darwin Finder osascript（trash_darwin.go，目的地可由脚本输出捕获）、linux 自研 XDG（dst 在 `uniqueXDG` 已知，trashinfo 已含原路径）、windows SHFileOperation（无法取得回收站目的地映射 → 回撤降级）。
- `MoveFile` 返回目标路径（move.go:22，当前执行器丢弃了返回值）；`HardlinkMerge` 后 dup 与 keep 指向同一 inode。
- 操作前校验体系完备：`VerifyFile`（size + 内容 BLAKE3 重算 + inode 身份）、`identityStill`（executor.go:153-182）。回撤复用同一安全哲学。
- cfgDir 下现状仅 `cache.db` + `settings.json`；cache.db 有 AlgoVersion bump 整库作废与损坏自愈逻辑——历史与操作日志绝不可放入该库（回撤账本被"缓存自愈"清空 = 数据安全事故）。
- 操作失败项：`ops.Options.TrashFn` 签名 `func(paths []string) error`，批量语义；失败回退逐文件重试（C6/S5/S7）。

## 2. 总体架构

```
┌─ frontend ────────────────────────────────────────────────┐
│ ScanView(roots/上次扫描横幅)  ResultView(多目录优先级策略、 │
│ 组内全选)  RecordsView(标签页: 扫描历史 | 清理记录+回撤)   │
└──────────────┬────────────────────────────────────────────┘
               │ wails.ts 手写桥
┌──────────────▼────────────────────────────────────────────┐
│ app.go (App 绑定层)                                        │
│  · 结果集生命周期 + curHistID + resultsReady               │
│  · ExecuteOperation 内嵌 journal 写前日志与逐项回写        │
│  · UndoOperation 异步执行 + 事件                           │
├───────────────┬──────────────────────┬────────────────────┤
│ internal/     │ internal/ops          │ internal/history   │
│ model         │  keep.go 多目录优先   │  (新包, SQLite)     │
│ KeepPolicy    │  executor.go 结果回调 │  history.db 单文件  │
│ 扩展          │  trash 返回 src→dst   │  扫描历史 + 操作日志 │
└───────────────┴──────────────────────┴────────────────────┘
```

新包 `internal/history`：唯一持有 `history.db` 的模块，对外两类 API——扫描历史（ScanStore）与操作日志（OpLog）。全部方法内部互斥串行化（单 `sync.Mutex` + `database/sql`），损坏时返回错误、App 层降级为「历史/回撤不可用」而非崩溃（与 cache 打开失败不阻塞同风格，但需显式 toast）。

## 3. 数据模型（history.db，schema v1）

`internal/history/schema.go`，PRAGMA 与 cache 同口径：`journal_mode=WAL`、`synchronous=NORMAL`、`foreign_keys=ON`。`user_version=1`，损坏走「删除重建」（历史数据可丢，绝不循环卡死）。

```sql
CREATE TABLE scan_history (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  saved_at     INTEGER NOT NULL,            -- Unix epoch 秒
  roots        TEXT    NOT NULL,            -- JSON []string
  filters      TEXT    NOT NULL,            -- JSON model.Filters
  threads      INTEGER NOT NULL DEFAULT 0,
  paranoid     INTEGER NOT NULL DEFAULT 0,
  groups_count INTEGER NOT NULL,
  files_count  INTEGER NOT NULL,            -- 当前存活口径（随清理递减）
  orig_files   INTEGER NOT NULL,            -- 保存时原始文件数（用于"已清理 x/y"）
  reclaimable  INTEGER NOT NULL,            -- 当前存活口径
  failed_json  TEXT    NOT NULL DEFAULT '', -- 扫描失败清单 []model.FailedItem
  keep_ids     TEXT    NOT NULL DEFAULT '[]'-- 保留决策 []int64（文件 ID）
);
CREATE TABLE hist_groups (
  id          INTEGER PRIMARY KEY AUTOINCREMENT, -- 直接复用为 DuplicateGroup.GroupID
  hist_id     INTEGER NOT NULL REFERENCES scan_history(id) ON DELETE CASCADE,
  hash        BLOB    NOT NULL,                  -- 32B 组内容哈希（VerifyFile 依据）
  size        INTEGER NOT NULL,
  reclaimable INTEGER NOT NULL,
  ord         INTEGER NOT NULL
);
CREATE TABLE hist_files (
  id        INTEGER PRIMARY KEY AUTOINCREMENT,   -- 直接复用为 FileEntry.ID（跨记录全局唯一）
  hist_id   INTEGER NOT NULL REFERENCES scan_history(id) ON DELETE CASCADE,
  group_id  INTEGER NOT NULL REFERENCES hist_groups(id) ON DELETE CASCADE,
  path      TEXT    NOT NULL,
  size      INTEGER NOT NULL,
  mtime_ns  INTEGER NOT NULL
);
CREATE INDEX idx_hist_files_hist ON hist_files(hist_id, group_id);
CREATE INDEX idx_hist_files_path ON hist_files(hist_id, path);

CREATE TABLE op_records (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  op_kind     TEXT NOT NULL,                    -- trash/delete/move/hardlink
  created_at  INTEGER NOT NULL,
  target_dir  TEXT NOT NULL DEFAULT '',
  hist_id     INTEGER NOT NULL DEFAULT 0,       -- 关联扫描历史（0=无）
  undoable    INTEGER NOT NULL DEFAULT 0,       -- 0=delete/Windows-trash；1=其余
  done_count  INTEGER NOT NULL DEFAULT 0,
  reclaimed   INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE op_items (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  op_id      INTEGER NOT NULL REFERENCES op_records(id) ON DELETE CASCADE,
  orig_path  TEXT NOT NULL,                     -- 操作时的源路径
  dest_path  TEXT NOT NULL DEFAULT '',          -- trash 目的地 / move 目标；hardlink 留空
  link_src   TEXT NOT NULL DEFAULT '',          -- hardlink：合并时保留源路径
  hash       BLOB    NOT NULL,                  -- 32B 组哈希（回撤内容级校验依据）
  size       INTEGER NOT NULL,
  mtime_ns   INTEGER NOT NULL,
  state      TEXT NOT NULL,                     -- 见 §7 状态机
  err        TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_op_items_state ON op_items(op_id, state);
```

要点：
- `hist_files.id` / `hist_groups.id` 用 rowid 直接充当文件/组 ID，恢复后 ID 稳定，`keep_ids` 以文件 ID JSON 存储即可跨会话还原。
- 淘汰：`SaveScan` 成功后 `DELETE FROM scan_history WHERE id NOT IN (SELECT id ... ORDER BY saved_at DESC LIMIT 20)`，CASCADE 清子表。常量 `MaxScanHistory = 20`。
- `op_records`/`op_items` 不自动淘汰（行小、追加慢），面板提供「清空记录」（确认框警示：清空后失去回撤能力）。
- 启动时把全库残留 `planned` 项置 `interrupted`（上次进程死于执行中）。

## 4. 功能一：保留策略多目录优先级

### 4.1 后端

- `model.KeepPolicy` 改为 `{Kind string; Directories []string}`（删除 `Directory`；仅此结构跨绑定传输，无兼容包袱）。
- `ops.ApplyKeepPolicy`：`"directory"` 分支 → `pickByDirectoryPriority(g, dirs)`：按 dirs 顺序（`filepath.Clean`，空串项跳过），对每个目录取组内匹配文件中的最深者（沿用现有最长前缀语义），首个命中的目录胜出；全部未命中 → 该组跳过（不产生决策）。dirs 为空 → 返回错误（绑定层校验）。
- `App.ApplyKeepPolicy(policy)`：Kind=="directory" 且 `len(Directories)==0` 时报错「请至少添加一个保留目录」；决策成功后把 keepIDs 写入当前历史记录（§5.2）。
- 目录匹配保持纯字符串前缀 + 分隔符边界（现状语义），不解析符号链接（与执行器安全校验分层，保留策略只是建议）。

### 4.2 前端（ResultView.vue 保留策略区）

- `keepKind=="directory"` 时显示有序目录列表（非单输入框）：
  - 行：`↑ ↓ ✕` + 完整路径；按钮「+ 添加目录」调 `api.selectDirectory()` 追加（去重）；支持粘贴路径回车添加（复用 ScanView roots 的输入模式，不引入拖拽）。
  - 排序=数组序，上移/下移即优先级调整；空列表时「应用策略」禁用并提示。
- `store.applyKeep(kind)` 签名改为传 `dirs: string[]`；`wails.ts` 桥 `applyKeepPolicy(kind, dirs)`。

## 5. 功能三：扫描历史

### 5.1 保存时机（App 层，全自动）

1. **扫描完成**：`scan:done` 且 `len(groups)>0` → 在 scan goroutine 中「持锁写完结果集→释放锁」之后、发 `scan:done` 事件之前，同步 `SaveScan(cfg, groups, failed)`（不持 `a.mu` 调 store，见 §8 锁序），事务批量插入；得到 `histID` 记入 `a.curHistID`。10 万行单事务插入实测为百毫秒级，可接受；保存失败不影响扫描结果展示，仅 toast「历史保存失败: …」并置 `curHistID=0`。
2. **清理后增量更新**：`ExecuteOperation` 收尾裁剪结果集后，若 `curHistID!=0`：`PruneHistoryFiles(histID, gonePaths)`——事务内删对应 `hist_files` 行、组内文件数 <2 的组删除、重算 `groups_count/files_count/reclaimable`。失败仅 stderr + toast，不影响操作结果。
3. **保留决策**：`ApplyKeepPolicy`/`ClearKeepDecisions` 成功后回写 `keep_ids`。
4. 恢复历史后继续清理 → 增量更新该条历史（不新建记录）；再次**扫描** → 生成新记录并切换 `curHistID`。

### 5.2 恢复（`LoadScanHistory(histID) (ScanSummary, error)`）

- 前置：`!opsRunning && !scanInFlight`；读取 `hist_groups/hist_files` 重建 `a.groups/a.byID`（ID 用存储 rowid；`FileEntry.ModTime=mtime_ns`；`DuplicateGroup.Hash` 用 BLOB），恢复 `keepIDs`、`failed`；置 `resultsReady=true`、`curHistID=histID`；返回组数/可释放量供前端切页。
- 恢复后 UI 顶部常驻提示条：「当前为历史结果（保存于 …），清理前会逐文件自动校验，已变/已删的文件会被拦截或跳过」。
- 历史条目间 ID 全局不冲突（AUTOINCREMENT 跨记录递增，删记录不回卷）。

### 5.3 其他绑定

- `ListScanHistory() ([]HistoryMeta, error)`：id、saved_at、roots、threads/paranoid/filters（供重扫回填 `StartScan`）、groups/files/orig_files、reclaimable；不携带操作日志字段。按 saved_at 降序。
- `DeleteScanHistory(id) error`、`ClearScanHistory() error`（清空同时把 `curHistID` 归零；当前结果集不受影响）。
- 「重新扫描」：前端用 meta 组装 `ScanConfig` 调既有 `StartScan`——不新增后端接口。

### 5.4 状态门槛改造（关键接口变化）

`ExecuteOperation` 的 `pipe.Status()==Done` 门槛改为 `a.resultsReady`：
- 置位：scan 成功写完结果集 / LoadScanHistory 成功；
- 复位：`StartScan` 开始、`CancelScan` 生效收尾（scan goroutine 的 err 分支）。
- `GetResultGroups` 等只读接口不查该标志（现状即如此，行为不变）。
- 该改造同时消除现状对「引擎状态恰为 Done」的依赖：历史恢复、或 Done 后引擎被复位等场景下，只要结果集就绪即可安全清理。

### 5.5 前端

- App.vue 导航加「记录」页 → `RecordsView.vue`，两个标签页：
  - **扫描历史**：表（时间、roots 摘要、组数、文件数(存活/原始)、可释放、操作列：恢复/重扫/删除），顶部「清空」。恢复成功 → `store.resultView` 切换 + toast。
  - 恢复走 `api.loadScanHistory(id)`，事件与状态全部复用现有 store 流。
- ScanView：`app:ready` 后若 `ListScanHistory` 非空，显示横幅「上次扫描：{时间} {roots 摘要} · 恢复最近结果 / 查看历史」；点恢复即 `loadScanHistory(最新id)`。

## 6. 功能二：组内全选

纯前端：
- `stores/scan.ts`：`toggleGroupSelection(group: GroupView)`——组内非 `isKeep` 文件已全部选中则全部取消，否则补齐选中；`groupSelectionState(group): 'none'|'some'|'all'`。
- `GroupCard.vue` 头部加三态复选框（all=✓，some=横杠，none=空），点击调 `toggleGroupSelection`；与全局 `Cmd+A`/「全选待清理」按钮并存互不干扰（同一 `selection` Set）。
- 不动后端；`ExecuteOperation` 的文件 ID 校验/保留项拦截（S2）不变。

## 7. 功能四：清理记录与回撤

### 7.1 写前日志（ExecuteOperation 内嵌，App 层编排）

时序（全部失败只降级记账、不阻断清理本身——清理结果优先于日志完整性，但日志失败必须显式 toast）：

1. 入口校验通过后、派发前：`OpLog.Begin(kind, targetDir, histID, items[]FileEntry+hash)` → 插入 `op_records`（undoable：trash 按平台、delete=0、move/hardlink=1）+ 全部 `op_items(state='planned')`。返回 `opLogID`。
2. 执行期：`ops.Execute` 新增结果回调 `OnItem func(origPath, destPath string, code ItemCode)`（ok/failed/skipped + err 文本 + trash 目的地），App 侧逐项 `OpLog.FinishItem`。取消（ctx）后未派发项在收尾统一置 `cancelled`。
3. 收尾：`ops:done` 前 `OpLog.Finalize(opLogID, doneCount, reclaimed)`；panic 路径经 `OnPanic` 也走 Finalize（残留 planned → `interrupted`）。

`ops` 包内部改造（对回撤最重要的两块）：
- **trash 返回目的地映射**：`Trash` 签名 `func(paths []string) error` → `func(paths []string) (map[string]string, error)`（src→dst，尽力填充）。darwin 脚本追加输出每个 moved item 的 POSIX 路径，Go 侧按行解析配对（新增纯函数 `parseTrashOutput`，单测覆盖）；linux 在 `trashXDG` 循环内已知 dst，直接填；windows 返回空 map（不改行为，undoable=0）。跨卷复制删除路径（moveIntoTrash）dst 同样已知。
- **move 记录目标**：执行器不再丢弃 `MoveFile` 返回值，`OnItem` 带出 dst。
- **hardlink**：`OnItem` 带出 `linkSrc=keep.Path`。
- 批量 trash 失败回退逐文件重试路径（C6）天然逐项拿 dst。

### 7.2 回撤 `UndoOperation(opLogID) (string, error)`

异步（goTask + `opsRunning` 互斥复用 + 进度事件 `ops:progress`，完成事件 `ops:undo:done`）。逐 `op_items WHERE state='done'` 执行，按 kind 分派；**每项独立成败，绝不中断整批**；成功后 state→`undone`，失败→`undo_failed`+err（可重试，幂等：只处理 done 项）。

| kind | 回撤动作 | 前置校验（不过则 undo_failed） |
|------|---------|------------------------------|
| trash (darwin/linux) | `dest → orig` rename（跨卷退化复制+删源，复用 `moveIntoTrash` 风格的 copyVerify）；还原 mtime_ns | dest 存在且为普通文件、size==记录；orig 不存在；**orig 已存在→不覆盖**，落地 `name.fdd-restored.ext`（uniqueDst 递增）；dest 缺失→Failed「回收站中已找不到（可能被清空）」 |
| move | `dest → filepath.Dir(orig)` 调 `MoveFile` | dest 存在、size==记录；orig 位被占由 uniqueDst 递增，不覆盖 |
| hardlink | 从 `link_src` 复制重建 `orig`：复制→tmp，**tmp 内容 BLAKE3==记录 hash** 才 rename 替换现有链接，再 chtimes(mtime_ns) | link_src 存在、size==记录；tmp hash 不符→删 tmp、Failed「保留源内容已变，拒绝恢复」；orig 已非链接（用户已另行处理）→Failed |
| delete | — | 记录上 UI 明示「永久删除，不可回撤」，按钮禁用 |
| trash (windows) | — | `undoable=0`，UI 显示「请到系统回收站还原」+ 复用 `OpenTrash` |

安全红线：回撤永不覆盖任何现有文件（唯一例外是 hardlink 替换的 dup 路径本身——它此刻仍是内容一致的硬链接，替换前已证 hash）；hardlink 项替换前复核 dup 路径当前 inode 与 link_src 同身份（`identityStill` 思想），若用户已把它改回独立文件则拒绝。

### 7.3 清理记录 UI（RecordsView 第二标签）

- 列表：时间、类型（回收站/永久删除/移动/硬链接）、条数、释放量、可回撤数/已回撤数、状态徽标；行展开 → 条目表（原路径 → 目的地，state，err）。
- 操作：「回撤」（确认框：条数 + 上表语义说明 + 不可回撤项计数提示）、「打开系统回收站」（windows/补充出口）、「清空记录」。
- 回撤进度复用 ops:progress 条；完成后 toast 汇总「恢复 x 项，失败 y 项」，列表状态即时刷新。
- 回撤不回写当前结果集（条目本已被裁剪）；提示「如需更新结果请重新扫描」——不做跨模块回填，避免伪需求复杂度。

## 8. 并发与错误处理边界

- `history.Store` 单互斥串行全部读写；App 只在绑定 goroutine / scan/ops goTask 中调用，无嵌套锁（`a.mu` 与 store 锁永不互逆持有：先从 a.mu 取快照→放锁→调 store）。
- `LoadScanHistory` 与 `StartScan`/`ExecuteOperation` 互斥（共用 `scanInFlight/opsRunning` 门槛 + 入口持 `a.mu` 检查）。
- history.db 打开失败：应用照常启动，历史/记录/回撤绑定统一返回「历史库不可用: err」，前端相应页显示错误态；`ExecuteOperation` 主流程不受影响（journal 降级为 no-op，结果事件带 warning 字段？否——保持简单：toast 一次性提示）。
- 路径含换行等特殊字符：AppleScript 目的地解析按行分帧——darwin trash 路径集合与输出行配对采用「数量不等即放弃映射」（不猜序），失败退化为该项 dest 未知→undo 标记「无法定位回收站位置」。XDG/move 路径无此问题。

## 9. 不做的事（YAGNI / 明确排除）

- fdd-cli 不接触历史/回撤/保留策略（保持 scan-only）。
- 不做回撤的细粒度（单条 item 级回撤按钮）、不做部分回撤选择性子集——整记录回撤，条目级失败已有明细。
- 不做历史结果打开时预校验、不做「恢复后继续操作」跨记录合并。
- Windows 应用内回收站回撤（留 schema dest 列即可后补）。
- 多语言、设置页历史上限调整、导出报告（沿用 M5 占位）。

## 10. 测试与验收

**Go 单测（internal/history 新包）**：schema 往返（save→list→load 结构/ID/keepIDs 一致）；淘汰 20 上限级联；PruneHistoryFiles 增量与组计数；Begin/FinishItem/Finalize 状态机；启动 planned→interrupted；损坏库删除重建。

**ops**：`pickByDirectoryPriority`（多目录顺序、最深匹配、全不命中跳过、空 dirs）；`parseTrashOutput` 纯函数；linux trashXDG 映射返回；executor `OnItem` 各 kind 载荷（trash dst、move dst、hardlink link_src+hash 校验失败路径）。

**App 层（app_*_test.go 风格）**：scan:done 自动存历史 + curHistID；ExecuteOperation 后 hist 计数递减 + op 日志逐态；LoadScanHistory 重建结果集且 ExecuteOperation 在 resultsReady 下可行（模拟重启后状态 Idle）；UndoOperation trash（用 XDG 注入 temp dir / darwin 用假 Trash 映射）与 move/hardlink 端到端 temp 文件用例；回撤不覆盖语义（原位放新文件→ `.fdd-restored` 落地断言）；delete/windows undoable=0。

**回归基线**（项目既定标准）：`gofmt`、`go vet ./...` ×3 平台、`go test -race -count=2 ./...`（history/ops 新包 `-count=4`）、`npm run build`(vite)、benchgen+fdd-cli 双跑冒烟；Linux 项由 CI 覆盖。

**UI 手动验收**（本机 darwin）：多目录列表排序→应用→组内保留者符合优先级；组内三态全选与全局全选互不踩踏；清理→重启→历史恢复→再清理→历史计数更新；清理记录→回收站回撤文件回原位、move 回撤、hardlink 回撤后内容独立且 hash 等值、delete 显示不可撤。

## 11. 文档与版本

- `docs/09-用户手册.md`（canonical）：§6.2 保留策略改写为多目录优先级；新增「扫描历史与清理记录」「回撤」章节；FAQ 补 Windows 回收站回撤与「历史结果是旧的」两条。
- 版本号 0.5.0：`AppVersion`（app.go）、`docs/09`、`package.json`（三处口径，与 0.4.0 惯例一致）。
- 冻结文档 01–03 不动。
