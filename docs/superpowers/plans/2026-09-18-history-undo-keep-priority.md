# 扫描历史 / 清理回撤 / 保留策略多目录 / 组内全选 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans（本计划已获用户预授权自动执行）。Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 FileDedup 增加四项功能：保留策略多目录优先级、重复组内全选、扫描历史持久化与恢复、清理记录与回撤。

**Architecture:** 新建 `internal/history` 包独占 `history.db`（SQLite，与哈希缓存 cache.db 物理隔离）；`ops` 包扩展（多目录保留、trash 目的地映射、逐项结果回调、回撤原语）；`app.go` 绑定层编排自动保存/写前日志/回撤异步执行；前端 Vue3+Pinia 增加 RecordsView（历史+清理记录两标签）并改造 ResultView/GroupCard。

**Tech Stack:** Go 1.x + modernc.org/sqlite（复用现有依赖）、Wails v2、Vue 3 `<script setup>` + Pinia、Vite。

**Spec:** `docs/superpowers/specs/2026-09-18-history-undo-keep-priority-design.md`

## Global Constraints

- 版本目标 0.5.0；版本号三处同改：`app.go` `AppVersion`、`wails.json` `productVersion`、`frontend/package.json` `version`，另 `docs/09-用户手册.md` 头部「适用版本」。
- 不新增 Go 依赖；SQLite 驱动沿用 cache 包所用的 `modernc.org/sqlite`（`sql.Open("sqlite", ...)`）。
- 回撤/清理绝不覆盖任何现有文件（唯一例外：hardlink 回撤替换的 dup 路径本身，替换前必须证 hash 相等）。
- `a.mu`（App 锁）与 `history.Store` 内部锁**永不嵌套**：先取快照→放锁→再调 store。
- 前端中文文案硬编码（项目无 i18n）；`go fmt` 全绿；每个任务收尾一次 commit（conventional style，中文摘要，参照 git log 风格）。
- 测试基线：每任务 `go test -race ./internal/... .`（相关包）+ 末尾任务全量回归（vet×3 平台、`-count=2`、`npm run build`、fdd-cli/benchgen 冒烟）。

## 文件结构总览

```
internal/model/model.go        KeepPolicy.Directories（改）
internal/ops/keep.go           pickByDirectoryPriority（改）
internal/ops/executor.go       TrashFn 签名、OnItem 回调（改）
internal/ops/trash.go/_darwin/_linux/_windows  目的地映射（改）
internal/ops/undo.go           UndoOne 回撤原语（新）
internal/ops/ops_test.go       多目录/映射解析测试（改）
internal/ops/undo_test.go      回撤测试（新）
internal/history/history.go    Store/Open/Close/损坏重建（新）
internal/history/scan.go       扫描历史 CRUD（新）
internal/history/oplog.go      操作日志 CRUD（新）
internal/history/*_test.go     （新）
app.go                         绑定/编排/resultsReady/历史与回撤接口（改）
app_history_test.go / app_undo_test.go（新）
frontend/src/wails.ts          新桥接方法（改）
frontend/src/stores/scan.ts    keepDirs/组内全选/历史状态（改）
frontend/src/views/ResultView.vue   多目录优先级 UI（改）
frontend/src/components/GroupCard.vue  三态全选框（改）
frontend/src/views/RecordsView.vue   历史+清理记录（新）
frontend/src/App.vue           导航项（改）
frontend/src/views/ScanView.vue     上次扫描横幅（改）
docs/09-用户手册.md             新章节（改）
```

---

## 阶段 A：保留策略多目录（功能 1）

### Task 1: ops 层多目录优先级保留

**Files:**
- Modify: `internal/model/model.go:100-104`（KeepPolicy）
- Modify: `internal/ops/keep.go`
- Test: `internal/ops/ops_test.go`（追加）

**Interfaces:**
- Produces: `model.KeepPolicy{Kind string; Directories []string}`（删除 `Directory` 字段）；`ops.ApplyKeepPolicy(groups []*model.DuplicateGroup, policy model.KeepPolicy) []KeepDecision` 签名不变。

- [ ] Step 1 写失败测试（追加到 ops_test.go，复用现有 `mkGroup`/entry 构造辅助，若无则按下列自建）：

```go
func TestKeepPolicyDirectoryPriority(t *testing.T) {
	g := &model.DuplicateGroup{GroupID: 1, Files: []*model.FileEntry{
		{ID: 1, Path: "/keep2/a/photo.jpg", Size: 10},
		{ID: 2, Path: "/keep1/x/photo.jpg", Size: 10},
		{ID: 3, Path: "/other/photo.jpg", Size: 10},
	}}
	// 优先级：keep1 > keep2；两目录都命中 → 保留 keep1 内文件
	ds := ApplyKeepPolicy([]*model.DuplicateGroup{g}, model.KeepPolicy{
		Kind: "directory", Directories: []string{"/keep1", "/keep2"}})
	if len(ds) != 1 || ds[0].KeepID != 2 { t.Fatalf("got %+v want keep 2", ds) }
	// 首位目录不命中 → 落到 keep2
	ds = ApplyKeepPolicy([]*model.DuplicateGroup{g}, model.KeepPolicy{
		Kind: "directory", Directories: []string{"/miss", "/keep2"}})
	if len(ds) != 1 || ds[0].KeepID != 1 { t.Fatalf("got %+v want keep 1", ds) }
	// 全不命中 → 该组跳过
	ds = ApplyKeepPolicy([]*model.DuplicateGroup{g}, model.KeepPolicy{
		Kind: "directory", Directories: []string{"/miss"}})
	if len(ds) != 0 { t.Fatalf("want skip, got %+v", ds) }
	// 同目录多命中 → 沿用最深匹配（现状语义）
	g2 := &model.DuplicateGroup{GroupID: 2, Files: []*model.FileEntry{
		{ID: 4, Path: "/d/a.jpg", Size: 9}, {ID: 5, Path: "/d/sub/a.jpg", Size: 9}}}
	ds = ApplyKeepPolicy([]*model.DuplicateGroup{g2}, model.KeepPolicy{
		Kind: "directory", Directories: []string{"/d"}})
	if len(ds) != 1 || ds[0].KeepID != 5 { t.Fatalf("deepest: got %+v", ds) }
	// 空串目录项被忽略
	ds = ApplyKeepPolicy([]*model.DuplicateGroup{g}, model.KeepPolicy{
		Kind: "directory", Directories: []string{"", "/keep1"}})
	if len(ds) != 1 || ds[0].KeepID != 2 { t.Fatalf("empty skipped: got %+v", ds) }
}
```

- [ ] Step 2 运行确认编译失败（`go test ./internal/ops/ -run DirectoryPriority` → undefined Directories）。
- [ ] Step 3 实现：`model.KeepPolicy` 改为 `{Kind string; Directories []string}`；`keep.go` 中 `case "directory": keep = pickByDirectoryPriority(g, policy.Directories)`，新函数 = 按序对每个非空目录调用现有 `pickInDirectory`（含 `filepath.Clean`），首个 `>=0` 结果胜出；删除旧单目录分支。全仓库编译修引用（`grep -rn "\.Directory\b" internal app.go`）。
- [ ] Step 4 `go test -race ./internal/ops/ && go build ./...` 全绿。
- [ ] Step 5 Commit：`feat(ops): 保留策略目录支持多目录优先级排序`

### Task 2: App 绑定 + 前端多目录优先级 UI

**Files:**
- Modify: `app.go:808-825`（ApplyKeepPolicy）
- Modify: `frontend/src/stores/scan.ts:239-246`、`frontend/src/wails.ts:197-198`、`frontend/src/views/ResultView.vue:65-72,115-132`

**Interfaces:**
- Consumes: Task 1 的 `model.KeepPolicy{Kind, Directories}`。
- Produces: 绑定 `ApplyKeepPolicy(policy)` 校验：`Kind=="directory" && len(Directories)==0` → error「请至少添加一个保留目录」；前端 `store.keepDirs: string[]`、`applyKeep(kind: string)`（内部按 kind 组装 Directories）。

- [ ] Step 1 app_test 追加失败用例：

```go
func TestApplyKeepPolicyDirectoryValidation(t *testing.T) {
	a, _ := newTestApp(t) // 现有测试辅助：注入 emit/临时 cfgDir，塞入结果集
	a.setTestGroups(sampleGroupsForTest()) // 若无此辅助，直接照抄现有测试构造 groups 的方式
	if _, err := a.ApplyKeepPolicy(model.KeepPolicy{Kind: "directory"}); err == nil {
		t.Fatal("空目录列表应报错")
	}
	if _, err := a.ApplyKeepPolicy(model.KeepPolicy{Kind: "directory",
		Directories: []string{"/x"}}); err != nil { t.Fatal(err) }
}
```

- [ ] Step 2 实现 app.go 校验分支；`go test -race . -run DirectoryValidation`。
- [ ] Step 3 前端：`scan.ts` 增 `keepDirs = ref<string[]>([])`、`addKeepDir()/removeKeepDir(i)/moveKeepDir(i, delta)`、`applyKeep` 传 `{kind, directories: keepDirs}`；`wails.ts` 的 `applyKeepPolicy(kind, dirs: string[])` 组装 `{Kind: kind, Directories: dirs}`（注意 model.KeepPolicy 无 JSON tag → 桥接层字段名用 Go 名，与现有 `Filters` 接口同风格）。
- [ ] Step 4 ResultView：`keepKind==='directory'` 时渲染有序列表行（路径 + ↑ ↓ ✕）+「添加目录」按钮（`api.selectDirectory()`，push 去重）+ 粘贴输入回车添加；空列表时「应用策略」disabled。样式沿用现有 .row/.btn 类。
- [ ] Step 5 `cd frontend && npm run build` 绿；本机 `wails dev` 手动冒烟（多目录排序→应用→保留者符合优先级）。
- [ ] Step 6 Commit：`feat(ui): 保留策略支持多目录选择与优先级排序`

## 阶段 B：组内全选（功能 2）

### Task 3: GroupCard 三态全选

**Files:**
- Modify: `frontend/src/stores/scan.ts`（selection 区）
- Modify: `frontend/src/components/GroupCard.vue:14,38-51`

**Interfaces:**
- Produces: store 新增 `toggleGroupSelection(g: GroupView)`、`groupSelState(g: GroupView): 'none'|'some'|'all'`（仅计非 isKeep 文件；全 keep 组返回 'none'）。

- [ ] Step 1 scan.ts 实现两个成员（纯函数式，基于现有 `selection: Set<number>`）：候选 = `g.files.filter(f => !f.isKeep).map(f => f.id)`；`all` 判定 every→state；toggle：all→delete 其余，否则 add 全部。
- [ ] Step 2 GroupCard 头部（组名行右侧）加 `<input type="checkbox">`：`:checked="state==='all'"`，`indeterminate` 经模板 ref 设置；`@change="store.toggleGroupSelection(group)"`；`title="全选/取消该组待清理项（不含保留项）"`。
- [ ] Step 3 `npm run build` 绿；手动冒烟：三态显示正确、与 Cmd+A 全局全选互不踩踏。
- [ ] Step 4 Commit：`feat(ui): 重复组内全选待清理项`

## 阶段 C：扫描历史（功能 3）

### Task 4: internal/history——库与扫描历史 CRUD

**Files:**
- Create: `internal/history/history.go`、`internal/history/scan.go`、`internal/history/history_test.go`、`internal/history/scan_test.go`

**Interfaces:**
- Produces（全部 task 5/7/9/11 依赖，签名冻结）：

```go
package history

const MaxScanHistory = 20
const (StatePlanned="planned"; StateDone="done"; StateFailed="failed";
	StateSkipped="skipped"; StateCancelled="cancelled"; StateInterrupted="interrupted";
	StateUndone="undone"; StateUndoFailed="undo_failed")

type Store struct{ mu sync.Mutex; db *sql.DB; path string }
func Open(path string) (*Store, error) // WAL/synchronous=NORMAL/foreign_keys=ON；建表；user_version=1；损坏→删库重建一次；启动把 planned 置 interrupted
func (s *Store) Close() error

type ScanMeta struct { ID, SavedAt int64; Roots []string; Filters model.Filters
	Threads int; Paranoid bool; Groups, Files, OrigFiles int; Reclaimable uint64
	Failed []model.FailedItem; KeepIDs []int64 }
func (s *Store) SaveScan(cfg model.ScanConfig, groups []*model.DuplicateGroup,
	failed []model.FailedItem) (int64, error) // 单事务插入+淘汰至 MaxScanHistory；返回 histID
func (s *Store) ListScans() ([]ScanMeta, error) // saved_at 降序；不含组明细
func (s *Store) LoadScan(id int64) (*ScanMeta, []*model.DuplicateGroup, error)
	// GroupID/文件 ID 复用存储 rowid（跨记录全局唯一，AUTOINCREMENT）
func (s *Store) UpdateKeepIDs(id int64, keepIDs []int64) error
func (s *Store) PruneScanFiles(id int64, gone map[string]bool) error
	// 删行→删 <2 组→重算 groups/files/reclaimable→keep_ids 裁剪至存活文件
func (s *Store) DeleteScan(id int64) error
func (s *Store) ClearScans() error
```

schema 照 spec §3（三表 + 两索引 + CASCADE）。

- [ ] Step 1 写失败测试 history_test.go：`TestOpenCreatesSchema`（t.TempDir 下 Open→SaveScan→Close→再 Open 数据在）、`TestCorruptRebuild`（写垃圾字节→Open 成功且为空库）、`TestStartupInterruptsPlanned` 留到 Task 7。scan_test.go：`TestSaveLoadRoundTrip`（2 组 5 文件+failed+keepIDs 往返，ID/Hash/ModTime 全等）、`TestEvictOldest`（存 22 条→ListScans 20 条且最旧两条的 hist_files 已级联清空）、`TestPrune`（删文件后计数/组删除/keep_ids 裁剪）、`TestDeleteAndClear`。
- [ ] Step 2 `go test ./internal/history/` 红。
- [ ] Step 3 实现（JSON 列用 encoding/json；roots/filters/failed/keep_ids 序列化；`SaveScan` 内 `cfg` 取 Roots/Filters/Threads/Paranoid）。
- [ ] Step 4 `go test -race ./internal/history/ -count=2` 绿。
- [ ] Step 5 Commit：`feat(history): 新增历史库与扫描历史持久化`

### Task 5: App 层接线（自动保存 / resultsReady / 恢复 / 增量更新）

**Files:**
- Modify: `app.go`（startup/shutdown、App 字段、StartScan 收尾、ExecuteOperation 门槛与收尾、ApplyKeepPolicy/ClearKeepDecisions、新绑定）
- Test: `app_history_test.go`（新）

**Interfaces:**
- Consumes: Task 4 全部 API。
- Produces:
  - App 新字段 `hist *history.Store; curHistID int64; resultsReady bool`（`hist` 访问一律经方法且不与 `a.mu` 嵌套）。
  - 绑定：`ListScanHistory() ([]HistoryMeta, error)`、`LoadScanHistory(id int64) (ScanSummary, error)`、`DeleteScanHistory(id int64) error`、`ClearScanHistory() error`。
  - `HistoryMeta{ID int64 json:"id"; SavedAt int64 json:"savedAt"; Roots []string json:"roots"; Filters model.Filters json:"filters"; Threads int json:"threads"; Paranoid bool json:"paranoid"; Groups int json:"groups"; Files int json:"files"; OrigFiles int json:"origFiles"; Reclaimable uint64 json:"reclaimable"}`（`HistoryMetaView` 命名可省，直接 `HistoryMeta`）。
  - `ExecuteOperation` 门槛：`a.pipe.Status()==Done` → `a.resultsReady`；`LoadScanHistory` 前置 `!opsRunning && !scanInFlight && a.hist!=nil`。

- [ ] Step 1 写失败测试（复用现有 newTestApp 风格，emit 注入捕获事件）：
  - `TestScanDoneSavesHistory`：跑桩扫描（现有测试如何构造 Done 结果集就复用）→ `ListScanHistory` 长度 1，字段吻合；
  - `TestExecuteOperationAfterHistoryLoadWithIdleStatus`：手工 `LoadScanHistory` 后 pipe 状态 Idle，`ExecuteOperation` 不再报「任务未完成」；
  - `TestExecuteOperationPrunesHistory`：trash 注入 temp（现有 ops 测试的 TrashFn 注入口/或 App 测试直接改 `ops.Trash`），清理后 `ListScanHistory` 的 Files/Groups 递减；
  - `TestLoadScanHistoryRebuildsIDs`：恢复后 `GetResultGroups` 出参与原扫描一致（组数、ID 集、isKeep 建议）。
  - 扫描历史关闭失败注入：`a.hist=nil` 时四绑定返回「历史库不可用」。
- [ ] Step 2 实现：startup `history.Open(cfgDir/history.db)`（失败 stderr 留痕，同 cache 风格）；shutdown Close；scan 成功收尾（放锁后、emit 前）`SaveScan`（err→stderr+toast 事件 `app:warn`？否——复用现有 toast 通道：emit `ops:done` 无关；简单方案：`fmt.Fprintf(os.Stderr)` + 前端不弹；spec 说 toast「历史保存失败」→ 发 `app:error` 事件，App.vue 已有 toast 桥则接，无则在 stores/scan.ts 事件订阅里加 `app:error` → toast；二选一，实现时看现有 toast 事件机制就近）；`resultsReady` 置/复位点全列：scan done true / scan err-cancel false / StartScan 入口 false / LoadScanHistory true。
- [ ] Step 3 `go test -race . -run 'History' -count=2` 绿；`go build ./...`。
- [ ] Step 4 Commit：`feat(app): 扫描历史自动保存、恢复与结果集门槛改造`

### Task 6: 前端历史 UI（RecordsView 标签一 + 横幅）

**Files:**
- Create: `frontend/src/views/RecordsView.vue`
- Modify: `frontend/src/App.vue`（导航）、`frontend/src/wails.ts`、`frontend/src/stores/scan.ts`（历史事件/恢复动作）、`frontend/src/views/ScanView.vue`（横幅）

- [ ] Step 1 wails.ts 增加 `listScanHistory/loadScanHistory/deleteScanHistory/clearScanHistory` + `HistoryMeta` 接口。
- [ ] Step 2 RecordsView：标签壳（本地 ref tab: 'scans'|'ops'，ops 标签 Task 12 填充，本任务先占位文案「清理记录将在后续步骤接入」——不允许，直接实现空态「暂无清理记录」渲染 ops 列表数据源留 []）；扫描历史表：时间（`new Date(savedAt*1000).toLocaleString('zh-CN')`）、roots 摘要（首个+«等 N 项»）、组数/文件数（files/origFiles 差异显示「已清理 x」）、可释放（复用 utils/format）、按钮组【恢复】【重扫】【删除】+ 顶部【清空】（ConfirmDialog 复用）。恢复成功→`store.setView('result')`+toast+横幅提示状态 `store.histResult: HistoryMeta|null`。重扫→`StartScan({Roots:meta.roots,...})` 后切扫描页。
- [ ] Step 3 ResultView 顶部：`store.histResult` 非空时提示条「当前为历史结果（保存于 …），清理前逐文件校验」。
- [ ] Step 4 ScanView 横幅：app 启动后 `listScanHistory()` 非空 → 显示「上次扫描 {time} {roots摘要} · 【恢复】【查看历史】」。
- [ ] Step 5 `npm run build` 绿；手动冒烟（扫描→重启应用→横幅恢复→清理→历史行文件数递减）。
- [ ] Step 6 Commit：`feat(ui): 扫描历史面板与恢复入口`

## 阶段 D：清理记录与回撤（功能 4）

### Task 7: internal/history——操作日志 CRUD

**Files:**
- Create: `internal/history/oplog.go`、`internal/history/oplog_test.go`
- Modify: `internal/history/history.go`（表已在 Task 4 schema 中建好——Task 4 建表必须含 op_records/op_items，本任务只加 API）

**Interfaces:**
- Produces（签名冻结）：

```go
type OpItemPlan struct{ OrigPath string; Hash [32]byte; Size uint64; MtimeNs int64 }
func (s *Store) BeginOp(kind, targetDir string, histID int64, undoable bool,
	items []OpItemPlan) (int64, error) // 全部 state=planned
func (s *Store) FinishItem(opID int64, origPath, destPath, linkSrc, state, errMsg string) error
func (s *Store) FinalizeOp(opID int64) error // 残留 planned→cancelled（正常收尾）；reclaimed/done 计数列更新
func (s *Store) MarkItemUndo(itemID int64, state, errMsg string) error // undone/undo_failed + err
func (s *Store) ListOps() ([]OpMeta, error) // created_at 降序 + 聚合计数
func (s *Store) GetOp(opID int64) (*OpMeta, []OpItem, error)
func (s *Store) ClearOps() error
type OpMeta struct{ ID int64; Kind string; CreatedAt int64; TargetDir string; HistID int64
	Undoable bool; Items, Done, Undone, Failed int; Reclaimable uint64 }
type OpItem struct{ ID int64; OrigPath, DestPath, LinkSrc, State, Err string
	Hash [32]byte; Size uint64; MtimeNs int64 }
```

- [ ] Step 1 失败测试：Begin→FinishItem(done)→Finalize 聚合；Finalize 前 planned→cancelled；MarkItemUndo 双态；`TestStartupInterruptsPlanned`（直接 INSERT planned 行后重 Open → 变 interrupted，补 Task 4 遗留项）；ClearOps 级联。
- [ ] Step 2 实现 + `go test -race ./internal/history/ -count=2` 绿。
- [ ] Step 3 Commit：`feat(history): 清理操作写前日志与状态机`

### Task 8: ops 包——trash 目的地映射 + OnItem 回调

**Files:**
- Modify: `internal/ops/trash.go`、`trash_darwin.go`、`trash_linux.go`、`trash_windows.go`、`executor.go`
- Test: `internal/ops/ops_test.go`、`internal/ops/trash_darwin_test.go`、`trash_linux_test.go`、`cancel_test.go` 等既有 TrashFn 桩修签名

**Interfaces:**
- Produces：

```go
// trash.go
var Trash = defaultTrash // func(paths []string) (map[string]string, error) src→dst，尽力填充；windows 恒空 map
// executor.go
type ItemResult struct{ OrigPath, DestPath, LinkSrc, State, Err string } // State: done/failed/skipped
type Options struct{ ...; OnItem func(ItemResult) } // nil 安全；每个进入校验/执行阶段的文件恰好回调一次
```

- [ ] Step 1 失败测试：
  - darwin `parseTrashOutput(stdout string, inputs []string) map[string]string`（纯函数：Finder `moved` 输出逐行 POSIX 路径；与输入 basename 多重集配对；**数量或基名对不上→返回 nil**，宁缺勿错配）；
  - linux：`trashXDG` 返回映射（temp trashDir 注入，断言 src→files/ 下 dst；重名递增 `.2` 后映射仍准确）；
  - executor：trash 成功批 → OnItem(done, dst)；verify Failed→failed、ENOENT→skipped；move → done+dst；hardlink → done+link_src；取消 → 未派发项不回调（由 App Finalize 归 cancelled）。
- [ ] Step 2 实现：darwin 脚本尾追加 `repeat with o in movedItems ... end repeat` 输出 moved item POSIX 路径（每行一个）；linux `defaultTrash/trashXDG` 收集 dst；windows 返回 `map[string]string{}`；executor：trash dst map、move dst（`MoveFile` 返回值落入并行 slice）、hardlink `linkSrc` 落入并行 slice，`aggregate()` 内逐文件 `opts.OnItem`（含 verify 阶段 Skipped/Failed 结果）。
- [ ] Step 3 既有测试全部修 TrashFn 签名；`go test -race ./internal/ops/ -count=4` 绿。
- [ ] Step 4 Commit：`feat(ops): 回收站目的地映射与逐项操作结果回调`

### Task 9: ExecuteOperation 挂接日志 + 历史增量

**Files:**
- Modify: `app.go`（ExecuteOperation）
- Test: `app_undo_test.go`（新，本任务先写 journal 部分）

- [ ] Step 1 失败测试：执行 trash 后 `ListOps` 出现记录（kind、items 数、done 计数、undoable 平台值）；op_items 含 dest；hardlink 记录 link_src；delete 记录 undoable=0；`hist` 为 nil 时清理仍正常（journal no-op）；`hist_id` 关联 curHistID 且 `PruneScanFiles` 生效（组合 Task 5 用例断言）。
- [ ] Step 2 实现：入口快照后、goTask 内 Execute 前构造 `[]OpItemPlan`（op.FileIDs → byID/组 hash；不在结果集的 id 不入计划）+ `BeginOp`；`OnItem` → `FinishItem`（done_count 累计与 reclaimed 在 Finalize 时由调用方传入？——`FinalizeOp(opID)` 内部由 items 聚合 done/reclaimed，无需外传）；Execute 返回后 `FinalizeOp`；结果集裁剪后 `PruneScanFiles`。undoable 计算：`trash → runtime.GOOS!="windows"`；`delete → false`；其余 true。
- [ ] Step 3 `go test -race . -count=2` 绿；Commit：`feat(app): 清理操作接入写前日志与历史裁剪`

### Task 10: ops/undo.go 回撤原语

**Files:**
- Create: `internal/ops/undo.go`、`internal/ops/undo_test.go`

**Interfaces:**
- Produces：

```go
type UndoItem struct{ Kind string // trash/move/hardlink
	OrigPath, DestPath, LinkSrc string; Hash [32]byte
	Size uint64; MtimeNs int64 }
func UndoOne(it UndoItem) (restoredPath string, err error) // 单文件独立成败
```

规则（spec §7.2 表）：trash——`DestPath` 存在且 size==记录，orig 被占则 `uniqueDst(dir(orig), base+".fdd-restored"+ext)`；rename 成功跨卷退化 copyVerify+删源（dest 侧）；chtimes(mtime_ns)。move——同 trash 但走 `MoveFile(DestPath, filepath.Dir(OrigPath))`（其 uniqueDst 天然不覆盖）。hardlink——OrigPath 当前 inode 必须与 LinkSrc 同身份（`fsid` 比较，未解析平台以「Lstat 双方存在且 size 相等」降级），LinkSrc 复制到 tmp→BLAKE3==Hash→rename 替换→chtimes；不符删除 tmp 且不动现状。

- [ ] Step 1 失败测试（temp dir 端到端）：trash 回原位；原位被占 → `.fdd-restored` 落地且占位文件不动；dest 缺失 → error；move 回迁；hardlink 正常拆链恢复独立文件且 hash 等值、LinkSrc 内容被改 → error 且 dup 现状不动、OrigPath 已被用户删除 → error「目标已不存在」；delete 类型传入 → error「永久删除不可回撤」。
- [ ] Step 2 实现（复用 move.go 的 copyVerify/uniqueDst、verify.go 的 identityStill 思路、hasher 池）。
- [ ] Step 3 `go test -race ./internal/ops/ -run Undo -count=4` 绿；Commit：`feat(ops): 回收站/移动/硬链接回撤原语`

### Task 11: App.UndoOperation 绑定

**Files:**
- Modify: `app.go`
- Test: `app_undo_test.go`（追加）

**Interfaces:**
- Produces: 绑定 `UndoOperation(opLogID int64) (string, error)`（返回 undoID）；互斥复用 `opsRunning/opsCancel`；事件复用 `ops:progress`，新增 `ops:undo:done` 载荷 `model.UndoResult{OpID int64; OK int; Failed []model.FailedItem; Restored []string}`（app.go 定义即可）。

- [ ] Step 1 失败测试：trash 记录（注入假 Trash 映射 temp 布局）回撤 → 文件回原位、`GetOp` items 变 undone、重复回撤幂等（第二次 0 项处理）；delete/windows trash（undoable=0）→ error「该记录不可回撤」；部分项失败不中断整批；`hist==nil` → error。
- [ ] Step 2 实现：`GetOp` 过滤 `state==done`（trash 且 DestPath=="" → 逐项 undo_failed「无法定位回收站位置」）；串行逐项 `UndoOne` → `MarkItemUndo`；进度 `onProgress`；收尾 emit `ops:undo:done`。不动结果集（文案提示重新扫描）。
- [ ] Step 3 `go test -race . -count=2` 绿；Commit：`feat(app): 清理记录回撤接口`

### Task 12: 前端清理记录标签 + 回撤交互

**Files:**
- Modify: `frontend/src/views/RecordsView.vue`、`frontend/src/wails.ts`、`frontend/src/stores/scan.ts`（undo 事件订阅）

- [ ] Step 1 wails.ts：`listOpRecords/getOpRecord/undoOperation` + `OpRecord/OpRecordItem` 接口（JSON tag 与 app.go 视图结构对齐：`{id,kind,createdAt,targetDir,undoable,items,done,undone,failed,reclaimable}`；item `{id,origPath,destPath,linkSrc,state,err}`）。
- [ ] Step 2 清理记录表：时间、类型徽标（回收站/永久删除/移动/硬链接）、条数、已回撤/失败、可回撤 →【回撤】（ConfirmDialog：n 项将被恢复，delete/Windows 提示不可撤）；行展开条目明细表；状态列（planned/interrupted 显示「中断」徽标）；`ops:undo:done` 与 `ops:done` 事件刷新列表；【打开系统回收站】（复用 OpenTrash）、【清空记录】二次确认警示「清空后无法回撤」。
- [ ] Step 3 `npm run build` 绿；本机冒烟：trash→回撤、move→回撤、hardlink→回撤后文件独立、delete 按钮禁用。
- [ ] Step 4 Commit：`feat(ui): 清理记录与回撤面板`

## 阶段 E：收尾

### Task 13: 文档、版本号与全量回归

**Files:**
- Modify: `docs/09-用户手册.md`、`app.go`（AppVersion）、`wails.json`、`frontend/package.json`
- [ ] Step 1 `docs/09`：§6.2 保留策略改写多目录优先级；新增 §6.6「扫描历史」、§6.7「清理记录与回撤」（含 Windows/ delete 不可撤说明、历史结果时效性）；FAQ 补两条。
- [ ] Step 2 版本三处 0.5.0 + 手册头部适用版本。
- [ ] Step 3 全量回归（项目既定基线）：`gofmt -l .`；`go vet ./...` × `GOOS=darwin/linux/windows`；`go test -race -count=2 ./...`（history/ops 追加 `-count=4`）；`cd frontend && npm run build`；`go run ./cmd/benchgen` 生成小语料后 `go run ./cmd/fdd-cli <dir>` 与 `-cache` 双跑冒烟。
- [ ] Step 4 Commit：`docs+release: v0.5.0 用户手册与版本号，历史/回撤/多目录保留策略`

### Task 14: 本机 UI 全功能验收（computer-use）

- [ ] 按 spec §10「UI 手动验收」清单跑一轮 Wails 应用实操（多目录排序应用、组内三态全选、清理→历史恢复→再清理→计数、回撤三类操作各一、delete 不可撤标注），记录并修复发现的问题；问题修复回插对应任务提交风格。

---

## Self-Review 结论

- Spec 覆盖：功能1→Task1/2；功能2→Task3；功能3→Task4/5/6；功能4→Task7–12；文档/版本/回归→Task13；UI 验收→Task14。§5.4 门槛改造在 Task5；§7.1 写前时序在 Task9；§8 锁序在 Global Constraints + Task5。
- 类型一致性：`ScanMeta/OpMeta/OpItem/ItemResult/UndoItem` 签名在 Task 间引用一致；`state` 常量以 `history.State*` 为唯一来源。
- 无占位符：Task6 ops 标签占位问题已改为渲染真实空态。
