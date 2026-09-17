# FileDedup 全面代码与文档审查报告

- 日期：2026-09-17
- 范围：Go 后端（internal/ 全模块、app.go、main.go、cmd/）、前端（frontend/src 全量）、技术文档（docs/ 14 篇）
- 基线：HEAD `077442d` + 已应用的 P0-P3 修复补丁 + 近期 2 处修复（move.go Windows 跨卷、pipeline.go 切片悬垂）
- 方法：4 路并行深度审查（Go 核心算法 / Go 操作层+绑定 / 前端 / 文档），均通过 `go build ./...`、`go test -race -count=1 ./...`、`GOOS=windows go build ./internal/ops ./internal/dedup` 验证

## 修复状态（2026-09-17 追加，全部逐项验证）

| 项 | 状态 | 修法摘要 | 回归测试 |
|---|---|---|---|
| R1 | ✅ 已修 | HardlinkMerge 改为「tmp 硬链接 → 备份 dup → 原子改名（失败恢复 backup）」 | `hardlink_merge_test.go`（含改名失败回滚） |
| R2 | ✅ 已修 | `trashXDG` 选名+写 info+移动整段加 `trashXDGGuard` 互斥 | `TestTrashXDGConcurrentSameName`（linux 专属，GOOS=linux 编译通过） |
| R3 | ✅ 已修 | store 加 `confirmOpen`，全局快捷键拦截确认态 | 手工验证 + esbuild 桩 |
| R4 | ✅ 已修 | docs/01 §2.2 Go 1.22+ → 1.27.1+ | 文档核对 |
| C1 | ✅ 已修 | 缓存命中后重算实际抽样，一致才信任缓存 full | `TestCacheMtimeContentChanged`（bug 态实测复现误报组） |
| C2 | ✅ 已修 | glob 区分 `*`（单层）与 `**`（递归），收紧 prunable | `TestPrunableC2Star` / `TestMatchPathStarVsDoubleStar` / `TestExcludeDirC2` |
| C3 | ✅ 已修 | 代表体替换只改 Path/Ext，保留 ID/Key | `TestPipelineHardlinkRepIDStable`（bug 态实测 ID=3 失败） |
| C4 | ✅ 已修 | Windows ResolveKey 改 os.Open + 全共享模式兜底重开 | GOOS=windows build+vet（本机无 Windows 运行时，已标注） |
| C5 | ✅ 已修 | GetStats 改读锁；计数触发式缓存，写入后失效 | `TestGetStatsCountsAfterWriteAndClear` / `TestGetStatsConcurrentWithWriters` |
| C6 | ✅ 已修 | trash 回退前 stat，源已不在按 S8 记 Skipped | `TestTrashFallbackSkipsAlreadyTrashed` |
| C7 | ✅ 已修 | runIndexed worker panic 守卫 + OnPanic → ops:error | `TestRunIndexedPanicGuard` / `TestExecuteWorkerPanicContained` |
| C8 | ✅ 已修 | osascript 改 argv 传参（静态脚本），路径零转义 | `TestTrashScriptIsStatic` / `TestOsascriptArgVFidelity` |
| C9 | ✅ 已修 | 前端补 `ops:error` 监听（复位 opsRunning + toast） | esbuild 桩 node 验证（复位+toast） |
| C10 | ✅ 已修 | ScanView addByPicker / FailedDrawer copyAll 补 catch | 构建通过（ResultView/ConfirmDialog 已由 P0-P3 补丁修复） |
| C11 | ✅ 已修 | moveTarget input 补 `aria-label` | 模板核对 |
| C12 | ✅ 已修 | bindEvents 返回清理函数（offEvent）+ App.vue 卸载解绑 + ResultView 卸载复位 confirmKind | esbuild 桩 node 验证（解绑 9 事件） |
| C13 | ✅ 已修 | `×` 字符分隔改 CSS 1px 竖线（aria-hidden） | 构建通过 |
| C14 | ✅ 已修 | docs/01 补 CancelOperation/ops:error/取消语义/ExecuteOperation 返回值 | 文档核对 |
| C15 | ✅ 已修 | optimize-report 建 G1–G12 三态台账（8 闭环 / 4 未实施） | 文档核对 |
| C16 | ✅ 已修 | docs/01 §5.1 追平 MaxThreads=64 + 介质自适应 | 文档核对 |
| C17 | ✅ 已修 | docs/01 §7.3 补 cache_meta/AlgoVersion/索引修正；§8 补 media 包；修订记录 V1.4 | 文档核对 |

集成验证：`go build/vet/gofmt` 全绿；`go test ./... -count=1` 与 `-race -count=1` 全绿；`GOOS=windows/linux` 交叉编译+vet 通过；`npm run build` 通过；`wails build` 打包成功。
遗留说明：R2/C4 的运行时行为需在对应平台（Linux CI / Windows 实机）做 `-race`/实跑确认，本机仅能交叉编译验证。

---

## 总览

| 级别 | 数量 | 性质 |
|---|---|---|
| 🔴 阻断 | 4 | 数据丢失 / 误删 / 构建阻断 |
| 🟡 需关注 | 17 | 正确性、跨平台、状态一致、契约、文档漂移 |
| 🟢 良好 | 多项 | 已验证无问题的关键路径 |

验证结论：当前代码**可编译、跨平台构建通过、基础并发路径 `-race` 无数据竞争**。问题集中在「极值/并发边界下的资源安全」与「设计主文档未追上实现」。

---

## 🔴 阻断级（必须修复）

### R1. `HardlinkMerge` 失败路径不可逆丢文件
- 位置：`internal/ops/move.go:60-68`
- 问题：`os.Remove(dup)` 成功后若 `os.Rename(tmp, dup)` 失败，当前 `os.Remove(tmp)` 把临时硬链接也删掉 → `dup` 已被删、`tmp` 也删 → **文件永久丢失**。
- 修复：失败分支应把 `tmp` 回滚还原为 `dup`（`os.Rename(tmp, dup)`），而非删除 `tmp`。

### R2. Linux 回收站并发竞态导致数据丢失
- 位置：`internal/ops/trash_linux.go:41-100` + `executor.go:224`
- 问题：批量 `trash` 失败回退走 `runIndexed(opWorkers=4)` 逐文件 `trashXDG`，其中 `uniqueXDG`(Stat)→`moveIntoTrash`(Rename) **非原子**。同名文件并发处理会选中同一 `dst` 互相覆盖 → 回收站条目被覆盖、源被删。
- 修复：`trashXDG` 内加进程内互斥（sync.Mutex），或 trash 全程串行。

### R3. 确认对话框打开时全局快捷键仍可改 selection（误删风险）
- 位置：`frontend/src/App.vue:19-39` + `ResultView.vue:60-64`
- 问题：`ConfirmDialog` 打开期间 `onKeydown` 仍响应 `Ctrl+A`/`Del`（仅判断 `view==='result' && !preview`，未排除 confirm 态）。确认时读 `store.selectedFiles` 未快照 → 弹窗展示「将处理 N 个文件」与实际执行集合可能不一致，**可能作用于非预期文件**。
- 修复：快捷键前置拦截 `!store.confirmOpen`（store 暴露确认态）；或确认时快照 `selectedFiles` 传入 `onConfirm`。

### R4. 设计主文档 Go 版本与真实工具链不符（构建阻断）
- 位置：`docs/01-项目设计文档.md` §2.2「后端 Go 1.22+」
- 问题：`go.mod` 为 `go 1.27.1`，README 也写 1.27.1+。按文档搭环境会直接编译失败。
- 修复：§2.2 改为「Go 1.27.1+」，与 go.mod / README 对齐。

---

## 🟡 需关注（中优先级）

### 算法 / 缓存
- **C1 缓存 mtime 信任可误报重复组** `cache.go:161` + pipeline：命中仅比对 `(path,size,mtime)` 即复用缓存 `full`；若内容变更但 `size+mtime` 不变（`cp -p`、COW、`FAT/exFAT` 2s 粒度、原地改写保 mtime），会把真正不同的文件判入同组 → **非 paranoid 下误删唯一文件**。建议：仅当本轮实算 `partial` 与缓存 `partial` 一致才信任 `full`；并在 UI 明示「改内容保 mtime 须清缓存/开 paranoid」。
- **C2 `ExcludeDir` 剪枝语义与 glob 预期不符** `filter.go:139-172` + `scanner.go:142`：尾随单 `*`（`a/b/*`）被当作递归前缀 → 连同 `a/b/c/d` 一并跳过（静默**多删**）；中间 `**`（`a/**/b`）仅匹配一层，`a/x/y/b` 不被剪枝（**少删**）。建议区分 `*`(单层) 与 `**`(递归)。
- **C3 FileKey 代表体 ID 被整结构体覆盖** `pipeline.go:241`：`*prev = *e` 把保留项 `ID`/`Key` 覆盖成被丢弃项 ID（注释称「ID 保持引用稳定」）。同指针无悬垂，但下游按 `ID` 关联选中态/历史会错位。建议仅复制 `Path`。
- **C4 Windows `ResolveKey` 开句柄 + 失败静默丢去重** `filekey_windows.go:40-44`：`syscall.Open/CloseHandle` 每候选开句柄，失败返回空 key → 硬链接不去重、`Reclaimable` 虚高。建议走 `os.Open` 复用，失败不应放弃去重。
- **C5 `GetStats` 取写锁 + 全表 COUNT** `cache.go:273`：已知 G11，应改 RLock + 触发式计数。

### 操作层 / 跨平台
- **C6 trash 回退重复处理状态不一致** `executor.go:215-233`：批量 trash 部分成功后返回 error，回退对**全部**条目重跑 `trash([单文件])`，已移入回收站的文件因源不存在记 `Failed`，但清理逻辑不移除 → 结果集仍显示「存在」而文件已在回收站。建议回退前记录已成功项、仅重试未处理项。
- **C7 `runIndexed` 无 panic 守卫** `executor.go:37-50`：`fn`（含 `trash`/`VerifyFile`/`HardlinkMerge`）panic 直接崩进程；`app.go` 的 `recoverGoroutine` 只包 `goTask`，覆盖不到 worker。建议在 worker 内 `defer recover` 并发回 `ops:error`。
- **C8 trash_darwin `%q` 引号脆弱** `trash_darwin.go:23`：`fmt.Sprintf("POSIX file %q", p)` 对含 `"`/`\`/换行的路径生成 AppleScript 不接受的转义 → 移回收站失败。建议用 `osascript` 传参或 `quoted form of (POSIX file …)`。
- **C9 前端缺 `ops:error` 监听** `stores/scan.ts:264`（后端发事件处）：后端 ops panic 时 `recoverGoroutine` 发 `ops:error`，前端无监听 → `opsRunning` 卡在 true，重演 P2「操作执行中」死锁。建议补 `onEvent('ops:error', …)` 复位并 toast。

### 前端健壮性 / 可访问性
- **C10 未捕获 rejection** `ScanView.vue:38`、`ResultView.vue:54`、`ConfirmDialog.vue:39` 的 `api.selectDirectory()`（dev 模式 `backend()` 同步抛错，`await` 后无 catch）；`FailedDrawer.vue:18` `navigator.clipboard?.writeText` 无 catch。
- **C11 `moveTarget` 缺可访问名** `ConfirmDialog.vue:86`：input 仅 placeholder，无 `label`/`aria-label`。
- **C12 `bindEvents` 未清理 + 切 Tab 残留 confirm** `App.vue:16`：`onMounted` 注册 `EventsOn` 无 `onUnmounted` 解绑，Vite HMR 重挂会重复绑定、toast 重复弹出；`confirmKind` 置于 `ResultView` 局部，切页时 `ConfirmDialog` 卸载但 `confirmKind` 不复位，切回应自动重开弹窗。
- **C13 字符图标** `GroupCard.vue:29`：用 `×` 字符作分隔符，违反图标一律 `<Icon>` 约定，建议改 CSS 或显式文本。

### 文档漂移
- **C14 缺 `CancelOperation` 与可取消清理语义** `docs/01` §3.2/§5.5：代码已落地 `CancelOperation() error`（app.go:296）、ops 可取消（executor Options.Ctx），且 `ExecuteOperation` 现 `return error`，文档未记录。
- **C15 optimize-report 计数与追踪缺口** `docs/optimize-report-2026-09-16.md` 开头「🟢 共 7 项」与 code-review 实际 G1–G12（12 项）不符；G7/G11 等未做也未显式「放弃」。建议建「已闭环/已顺带闭环/明确放弃」三态台账。
- **C16 线程默认值未反映介质自适应/上限** `docs/01` §5.1「1~核数」与 `pipeline.go:649 MaxThreads=64` + `autoThreads` + media 对 HDD/网络卷降级（G6）不符。
- **C17 缓存 schema 与目录结构滞后** `docs/01` §7.3 缺 `cache_meta` 表与 `AlgoVersion` 失效清空机制；§8 缺 `internal/media` 包；近期 Windows 跨卷 / 切片悬垂补丁未写入任何修订记录。

---

## 🟢 已验证良好 / 建议

- ✅ **切片悬垂已修复**：所有入 `pending` 的 `Full` 均为堆拷贝（`make`+`copy`），无 `x[:]` 栈数组别名隐患。
- ✅ **Wails 契约自洽**：`app.go` 方法签名、`ResultQuery`/`ScanConfig`/`GroupView`/`FileView`/`Settings`/`CacheStats` 的 json tag 与 `wails.ts` 完全对应；分页 0-based 无 off-by-one；控制方法已 `return error` 且前端捕获。
- ✅ **无 XSS 面**：grep 确认源码无 `v-html`/`innerHTML`；`MarkdownView`/`MdInline`/`PreviewPanel` 全部结构化插值渲染；`safeHref` 正确拦截 `javascript:/data:/vbscript:`。
- ✅ **状态管理正确**：`selection` 用 `ref(new Set())` 原地 mutation 可被追踪；`loadResultPage` 以 `resultChain` 串行化防交错；`useModal` 三处浮层聚焦/Tab 循环/归还焦点齐全；全局 `:focus-visible` 与 `prefers-reduced-motion` 已落地。
- ✅ **UI「19 项缺陷」自洽**：ui-review（P0×4/P1×7/P2×8=19）与 ui-fix（19+1）计数一致且代码对应落地。
- 💡 `crossdevice_windows.go:16` 的 `syscall.Errno(0x11E)` 实测可用（**勿**改 `x/sys/windows.ERROR_NOT_SAME_DEVICE`，类型不同 `errors.Is` 反而不匹配）；建议改用命名常量 `syscall.ERROR_NOT_SAME_DEVICE` 提升可读性。
- 💡 `media/probe_darwin.go:203` 子进程超时错误被永久缓存为 Unknown（瞬时超时即丧失旋转盘降级）；`move` 串行分支取消时未 `report` 剩余项（进度略少）。均为低危。

---

## 修复优先级建议

1. **R1 + R2（数据丢失）**：先修，二者都在「操作执行」路径，风险最高。
2. **R3（误删）**：前端确认态隔离，避免危险操作作用于错误集合。
3. **C1（缓存误报重复组）**：算法层误删风险，建议下个迭代优先。
4. **C7 + C9（panic 守卫与 ops:error 监听）**：补齐 P2 死锁的最终防线。
5. **R4 + C14~C17（文档）**：设计主文档追平实现，消除贡献者构建阻断与契约理解偏差。
6. **其余 🟡**：随版本迭代清理，无即时阻断风险。
