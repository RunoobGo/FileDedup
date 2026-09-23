// Wails 桥接层：类型定义 + window.go / window.runtime 包装（04 M2-T03）。
// 类型与 Go 侧 model / app.go 保持一致（01 §7 契约）。

export interface Filters {
  IncludeExts: string[]
  ExcludeExts: string[]
  MinSize: number
  MaxSize: number
  ExcludePaths: string[]
  // ExcludeDirs 精确目录排除（与 ExcludePaths 的 glob 分属两条通道）：
  // 命中该目录及其整个子树即在遍历层剪枝；条目来自目录选择器。
  // Go 侧零值（缺省/空）＝不排除，安全侧；显式列出同 AllowCloudHydration 的理由。
  ExcludeDirs: string[]
  IncludeHidden: boolean
  // M6-P1 云端占位：true = 照常读取（隐式触发按需下载）且不计数；
  // false = 跳过并计入 skippedCloudFiles。Go 侧零值即安全档，但这里必须
  // 显式列出：TS 的对象字面量缺字段会被当成 undefined 下发，Go 解出 false
  // 恰好也是安全档——写出来是为了让"新增一档语义"这件事在前端可见。
  AllowCloudHydration: boolean
}

export interface ScanConfig {
  Roots: string[]
  Filters: Filters
  Threads: number
  Paranoid: boolean
  UseCache: boolean
}

export interface ProgressEvent {
  Stage: string
  FilesDone: number
  FilesTotal: number
  BytesDone: number
  BytesTotal: number
  SpeedBps: number
  ETASeconds: number
}

export interface StageEvent {
  Stage: string
  Desc: string
}

export interface ScanSummary {
  groups: number
  reclaimable: number
  // 实占口径合计（M6-P2）：与 reclaimable 并列而非替换——后者是历史表与既往
  // 清理数字的口径，换语义会让"上次 8 GB 这次 300 MB"看起来像回归。
  // 两数之差就是稀疏/压缩文件被逻辑口径虚报的部分。
  reclaimableActual: number
  filesFailed: number
  elapsed: string
  // 系统保护清单的可见计数（M6-P4）：被剪枝的目录数 / 被跳过的盘根伪文件与
  // Windows 保留名文件数。引擎内置排除不许静默——结果比盘上少就得有地方说明。
  protectedDirs: number
  protectedFiles: number
  // 本轮因"云端占位"跳过的文件数（M6-P1）。AllowCloudHydration=true 时恒为 0。
  // ★ history.db 没存这三项（含下面两条），从记录页恢复时只能显示"未统计"，
  //   不得用零值冒充"这一轮没有云端文件"。
  skippedCloudFiles: number
  // 本轮按 worktemp.IsTempName 跳过的工作临时名文件数（M21）。
  // ★ 判据是名字形态，分不出"我们的残留"与"用户恰好这样命名的文件"。
  skippedWorkTempFiles: number
  // 本轮问卷过、但卷大小写语义取自平台默认的扫描根数（M62+M85）。
  // ★ 0 有两种成因（所有根都拿到读数 / 单根一趟按 C1 压根没问卷），不许读成"实测过"。
  // ★ 本条只是**类型位**：Go 侧有这个 JSON 字段，M30 比对器要求 TS 一侧同步补齐；
  //   任何 View 都不读它（裁定③「新增计数的界面呈现属 M8，不做」）。
  caseProbeUnproven: number
  // 非空即表示"这一轮有扫描根脱离了系统保护"（用户显式点名了保护清单内路径）。
  unprotectedRoots?: string[]
}

// HistoryMeta 扫描历史列表项（与 app.go HistoryMeta json tag 一致）。
// filters 为后端原始口径（字节），重扫时需 KB/字节换算（store.rescanHistory 负责）。
export interface HistoryMeta {
  id: number
  savedAt: number // Unix 秒
  roots: string[]
  filters: Filters
  threads: number
  paranoid: boolean
  groups: number
  files: number
  origFiles: number // 保存时文件数（与 files 差异 = 已清理量）
  reclaimable: number
}

export interface FileView {
  id: number
  path: string
  name: string
  size: number
  mtime: number
  isKeep: boolean
  // 所在卷标识（app.go FileView.Volume）。前端只做相等比较，不解释内容。
  // 用途：判断重复组是否跨卷，从而决定显不显示「软链接合并」。
  volume: string
  // volume 是否来自真实卷身份。false 时不可用于跨卷判定——
  // Windows 盘符不等于卷（挂载点会把别的卷挂到某目录下），
  // 拿路径前缀猜会把用户引向注定失败的按钮。见 isGroupCrossVolume。
  volumeResolved: boolean
}

export interface GroupView {
  groupID: number
  reclaimable: number
  // 实占口径（M6-P2）。actualKnown=false 表示组内**没有任一成员**读到过实占
  // （历史恢复、或该卷不提供 st_blocks/压缩尺寸）：此时 reclaimableActual 完全
  // 来自逻辑回退，界面须显示"实占未统计"而不是这个数。部分成员 unknown 时
  // 按逻辑大小计入，数字仍是可信下界（宁可少说，不把未知算成 0）。
  reclaimableActual: number
  actualKnown: boolean
  size: number
  files: FileView[]
}

export interface ResultQuery {
  page: number
  pageSize: number
  sort: string
  ext: string
}

export interface PagedResult {
  total: number
  page: number
  totalReclaimable: number // 全量口径（含未加载页，与 Go json tag 一致）
  // 实占口径的全量合计（M6-P2），口径同上。注意它**可能大于** totalReclaimable：
  // 实占含块对齐与预分配，逻辑大小不含。
  totalReclaimableActual: number
  groups: GroupView[]
}

export interface FailedItem {
  Path: string
  Stage: string
  Err: string
}

export interface Settings {
  threads: number
  filtersDefault: Filters
  theme: string
  language: string
}

export interface KeepDecision {
  groupID: number
  keepID: number
  removeIDs: number[]
}

// 保留策略结果（app.go KeepOutcome）。unmatchedGroups > 0 表示有组在
// 「按目录保留」下一条保留者都没标出来，这些组不受保护，必须提示用户。
export interface KeepOutcome {
  decisions?: KeepDecision[]
  unmatchedGroups: number
}

export interface OpsProgress {
  Done: number
  Total: number
  Current: string
}

// OpsResult 清理操作聚合结果（Go 侧 model.OpsResult）。
//
// **四个**字节口径互不重叠，UI 必须分别表述（见 ResultView.vue 结果条）：
//   Reclaimed      真正从磁盘移除的数据量（delete 与跨卷 move 出卷）
//   TrashedBytes   移入回收站的数据量（文件仍在磁盘上，清空回收站后才释放）
//   LinkedBytes    硬链接合并：数据未释放，只是不再重复存第二份
//   SymlinkedBytes 软链接合并：磁盘上少了一整份数据（dup 位置只剩链接对象），
//                  但保留项的数据块并未共享，且回撤要重新占回这块空间
//
// 为什么软链接不并进 Reclaimed：数值上它确实"释放了一整份文件"，
// 但把它与 trash/delete 混在一起，UI 就再也无法单独提示"链接有悬空风险"。
//
// 为什么 trash 也不并进 Reclaimed（2026-09-21，M22）：回收站不是"已消失"——
// 同卷进回收站只是一次改名，跨卷只是把数据搬到另一个卷的回收站，磁盘总量都未减。
// 修正前它落在 Reclaimed 里，结果条因此写"释放 X"，而紧挨着的按钮是"打开回收站"。
export interface OpsResult {
  OK: string[]
  Failed: { Path: string; Stage: string; Err: string }[]
  Skipped: string[]
  Cancelled: string[] // P2：取消后未派发的条目（未处理，仍在结果集中）
  Reclaimed: number // 已从磁盘真正释放的字节（delete / 跨卷 move 出卷）
  TrashedBytes?: number // 移入回收站的字节（2026-09-21 M22）：清空回收站后才释放
  LinkedBytes?: number // 硬链接合并涉及的字节：当期不释放空间，仅变为共享
  SymlinkedBytes?: number // 软链接合并涉及的字节（2026-09-20）
  Warnings?: string[] // 操作已成功、但需告知用户的情况（如临时文件残留未删净）
}

// OpKind 清理操作类型。'symlink' 为跨卷软链接合并（2026-09-20 新增），
// 与 'hardlink' 同为"链接类"合并，但可跨卷、需要权限、有悬空风险。
export type OpKind = 'trash' | 'delete' | 'move' | 'hardlink' | 'symlink'

export interface OpRequest {
  Kind: OpKind
  FileIDs: number[]
  TargetDir?: string
  ConfirmDanger?: boolean
  // ProcessDirs 处理策略（2026-09-20 新增）：仅本次操作生效的
  // 「优先处理的文件夹」。非空时实际处理范围 = FileIDs ∩ {位于这些目录下的文件}。
  //
  // 留空/不传 = 未启用，后端走与新增本字段之前完全一致的代码路径。
  // 注意它**不会**改变用户的勾选状态：显式勾选是用户的明确表达，
  // 一个策略设置不该悄悄改写它——后端也只收窄本次操作，不回写 selection。
  ProcessDirs?: string[]
  // ExcludeDirs 「不处理的文件夹」黑名单（2026-09-23 功能 2 新增）：
  // 实际处理范围再减去位于这些目录下的文件。与 ProcessDirs 同一内核
  // （pendingIDsLocked），短路序 gone→keep→outside→excluded。
  // ★ 不影响保留判定：黑名单内文件仍可当保留锚点、仍在结果集，
  // 唯一效果是永不进拟处理集。空/不传 = 未启用。
  ExcludeDirs?: string[]
}

// OpsFiltered 处理策略收窄了实际执行范围（后端事件 ops:filtered）。
//
// selected 是过滤**前**的勾选数，matched 是真正会处理的项数，两者之差就是
// 因为不在优先文件夹内而落空的项。UI 必须把这两个数都摆出来——
// 只说 matched，用户会以为剩下的没被选上；只说 selected，用户会以为都处理了。
export interface OpsFiltered {
  matched: number
  selected: number
  // unmatched 一个可处理文件都没命中的优先文件夹（回显用户输入的原文）。
  // 多半是路径写错，必须点名，否则用户以为策略已生效。
  unmatched: string[]
}

// ---------- v0.5.0 功能 4：清理记录与回撤 ----------

// OpRecord 清理操作摘要（与 app.go history.OpMeta json tag 一致）。
// done 为「曾执行成功」口径（含其后被回撤的项），剩余可撤 = done - undone。
export interface OpRecord {
  id: number
  kind: string // trash/delete/move/hardlink/symlink
  createdAt: number // Unix 秒
  targetDir: string
  histId: number
  undoable: boolean
  items: number
  done: number
  undone: number
  failed: number // 执行失败（不含回撤失败）
  reclaimable: number
}

export interface OpRecordItem {
  id: number
  origPath: string
  destPath: string
  linkSrc: string
  state: string // planned/done/failed/skipped/cancelled/interrupted/undoing/undone/undo_failed
  err: string
  size: number
  mtimeNs: number
  // 软链接条目专用（历史记录页展示用，2026-09-20）。
  //
  // 为什么放在历史条目上而不是实时查询文件系统：历史记录页要展示**当时**
  // 那次操作的结果，而链接可能早已因为保留项被删/盘被拔出而悬空。
  // 实时查询会把"当时是好的"显示成"坏的"，用户无法理解发生了什么。
  //
  // 后端在 GetOpRecord 时对该条目做一次检测：
  //   isSymlink = true 表示 OrigPath 当时/现在是一个符号链接（链接类条目）
  //   dangling  = true 表示链接的目标当前不可达（悬空，需用户处理）
  isSymlink?: boolean
  dangling?: boolean
}

export interface OpRecordDetail {
  meta: OpRecord
  list: OpRecordItem[]
}

export interface UndoResult {
  opId: number
  ok: number
  restored: string[]
  failed: { Path: string; Stage: string; Err: string }[]
}

export interface CacheStats {
  entries: number
  withFull: number
  dbSizeBytes: number
  lastEvicted: number
}

export interface PreviewData {
  kind: string
  content: string
  mimeType?: string
}

// ProcessPreview 是 PreviewProcessPolicy 的返回体（Go 侧 app.go 同名结构体）。
// 语义：处理策略只做执行时过滤、不改动勾选，所以"实际会处理哪些"在界面上本来
// 不可见——这个类型就是那份缺失的可见性。目前前端尚无调用点（呈现属 M8），
// 但类型面必须存在：缺了它，方法声明就引用了一个不存在的类型（G10/M30 同族漏网）。
export interface ProcessPreview {
  // 勾选项中位于优先文件夹内的 ID（Go 侧已与服务端快照求过交集）
  effectiveIds: number[]
  effectiveCount: number
  // 一个可处理文件都没命中的目录（用户写了个不存在的或空的路径，靠这个字段才看得见）
  unmatchedDirs: string[]
}

// ---------- 拟处理清单查询（2026-09-23，Go 侧 app.go 同名结构体逐字段镜像） ----------

export interface PendingQuery {
  selectedIds: number[]
  dirs: string[]
  excludeDirs: string[] // 「不处理的文件夹」黑名单（功能 2）；空=未启用
  page: number
  pageSize: number
  sort: string // group(默认)/size/path
}

export interface PendingRow {
  id: number
  path: string
  name: string
  size: number
  groupId: number
  pending: boolean
  reason: string // ''/keep/outside/gone/excluded，见 Go 侧 Pending* 常量
}

export interface PendingPage {
  total: number
  page: number
  pageSize: number
  pendingCount: number
  keepCount: number
  outsideCount: number
  goneCount: number
  excludedCount: number // 落在「不处理的文件夹」黑名单内的行数（功能 2）
  rows: PendingRow[]
}

// Wails 注入对象（最小接口约束，替代 any 边界；响应字段与 Go json tag 一致）
export interface BackendAPI {
  SelectDirectory(): Promise<string>
  StartScan(cfg: ScanConfig): Promise<string>
  PauseScan(): Promise<void>
  ResumeScan(): Promise<void>
  CancelScan(): Promise<void>
  GetStatus(): Promise<string>
  GetScanProgress(): Promise<ProgressEvent>
  GetResultGroups(q: ResultQuery): Promise<PagedResult>
  GetFailedItems(): Promise<FailedItem[]>
  PreviewFile(id: number): Promise<PreviewData>
  // PreviewProcessPolicy 只读预览"处理策略实际会命中哪些"（无副作用、不写账本、
  // 不动文件系统，因此不受 opsRunning 互斥限制）。声明在方法面钉子里（G5）：
  // 缺声明 = 后端有、前端按类型调不到。
  PreviewProcessPolicy(dirs: string[], excludeDirs: string[], selectedIDs: number[]): Promise<ProcessPreview>
  // GetPendingFiles 分页查询"执行前拟处理清单"明细（与上一绑定同一内核的两个投影：
  // 预览给计数、清单给逐行可读明细 + 被排除项的 reason）。同为只读，不受
  // opsRunning 互斥限制。
  GetPendingFiles(q: PendingQuery): Promise<PendingPage>
  RevealInFolder(id: number): Promise<void>
  GetSettings(): Promise<Settings>
  SaveSettings(s: Settings): Promise<Settings>
  GetVersion(): Promise<string>
  // GetStartupNotice 启动阶段的一次性提示（无则空串）。M12b：历史库影像损坏
  // 被隔离重建时，后端在 startup 里就知道，但那时前端还没注册监听器，
  // 事件必丢——所以由前端初始拉取取走。
  GetStartupNotice(): Promise<string>
  ListScanHistory(): Promise<HistoryMeta[]>
  LoadScanHistory(id: number): Promise<ScanSummary>
  DeleteScanHistory(id: number): Promise<void>
  ClearScanHistory(): Promise<void>
  ApplyKeepPolicy(policy: { Kind: string; Directories: string[] }): Promise<KeepOutcome>
  ClearKeepDecisions(): Promise<void>
  ExecuteOperation(op: OpRequest): Promise<string>
  // FilterInDirs 后端判定「paths 里哪些位于任一优先目录下」，返回命中的下标。
  // 前端不再自己比路径（AS-H6）：见 api.filterInDirs 上方注释。
  FilterInDirs(dirs: string[], paths: string[]): Promise<number[]>
  CancelOperation(): Promise<void>
  OpenTrash(): Promise<void>
  ListOpRecords(): Promise<OpRecord[]>
  GetOpRecord(id: number): Promise<OpRecordDetail>
  UndoOperation(opLogId: number): Promise<string>
  UndoOperationItem(opLogId: number, itemId: number): Promise<string>
  ClearOpRecords(): Promise<void>
  CacheStats(): Promise<CacheStats>
  CacheClear(): Promise<void>
  // ExportReport **目前是 M5 空桩**：Go 侧固定返回错误「报告导出将在 M5 提供」
  // （app.go 的 ExportReport）。声明在此只为了让方法面与 Go 对齐（G5），不代表
  // 功能可用——调用必然 reject。实现落在 M9，届时签名可能变化（format/path → 返回值），
  // 那时由方法面钉子与类型面钉子同时把关，不许 TS 先乐观。
  ExportReport(format: string, path: string): Promise<string>
}

declare global {
  interface Window {
    // Wails v2 注入路径为 window.go.main.App（结构体名大写，见 wailsjs/go/main/App.js
    // 生成的 window['go']['main']['App']）；app 小写为历史错误写法，保留兜底。
    go: { main: { App?: BackendAPI; app?: { App: BackendAPI } } }
    runtime?: {
      EventsOn: (name: string, cb: (data: any) => void) => void
      OnFileDrop?: (cb: (x: number, y: number, paths: string[]) => void, useDropTarget?: boolean) => void
      OnFileDropOff?: () => void
    }
  }
}

// 后端可用性探测：纯浏览器（vite dev）时 window.go 不存在。
// 路径修复（2026-09-17）：Wails v2 实际注入 window.go.main.App（结构体名大写），
// 旧代码探测 window.go.main.app.App 永远为 undefined，导致原生窗口内
// 所有后端调用抛「后端不可用」。
const backendOrNull = (): BackendAPI | null => {
  if (typeof window === 'undefined') return null
  const main = window.go?.main as { App?: BackendAPI; app?: { App: BackendAPI } } | undefined
  if (!main) return null
  return main.App ?? main.app?.App ?? null
}

// 调用入口：后端缺失时抛出明确错误（替代此前 null 解引用的 TypeError）
const backend = (): BackendAPI => {
  const b = backendOrNull()
  if (!b) throw new Error('后端不可用（浏览器开发模式下请使用 Wails 运行）')
  return b
}

export const api = {
  selectDirectory: (): Promise<string> => backend().SelectDirectory(),
  startScan: (cfg: ScanConfig): Promise<string> => backend().StartScan(cfg),
  pauseScan: (): Promise<void> => backend().PauseScan(),
  resumeScan: (): Promise<void> => backend().ResumeScan(),
  cancelScan: (): Promise<void> => backend().CancelScan(),
  getStatus: (): Promise<string> => backend().GetStatus(),
  getScanProgress: (): Promise<ProgressEvent> => backend().GetScanProgress(),
  getResultGroups: (q: ResultQuery): Promise<PagedResult> => backend().GetResultGroups(q),
  getFailedItems: (): Promise<FailedItem[]> => backend().GetFailedItems(),
  previewFile: (id: number): Promise<PreviewData> => backend().PreviewFile(id),
  revealInFolder: (id: number): Promise<void> => backend().RevealInFolder(id),
  getSettings: (): Promise<Settings> => backend().GetSettings(),
  saveSettings: (s: Settings): Promise<Settings> => backend().SaveSettings(s),
  getVersion: (): Promise<string> => backend().GetVersion(),
  getStartupNotice: (): Promise<string> => backend().GetStartupNotice(),
  listScanHistory: (): Promise<HistoryMeta[]> => backend().ListScanHistory(),
  loadScanHistory: (id: number): Promise<ScanSummary> => backend().LoadScanHistory(id),
  deleteScanHistory: (id: number): Promise<void> => backend().DeleteScanHistory(id),
  clearScanHistory: (): Promise<void> => backend().ClearScanHistory(),
  applyKeepPolicy: (kind: string, dirs: string[] = []): Promise<KeepOutcome> =>
    backend().ApplyKeepPolicy({ Kind: kind, Directories: dirs }),
  clearKeepDecisions: (): Promise<void> => backend().ClearKeepDecisions(),
  executeOperation: (op: OpRequest): Promise<string> => backend().ExecuteOperation(op),
  // filterInDirs 「这条路径在不在优先文件夹里」的唯一权威（AS-H6 / 决策 D-1 方案 b）。
  //
  // 为什么必须问后端而不是前端自己算：执行范围由后端 ops.inDir 决定，前端此前
  // 另写了一份 dirContains（按路径形状猜大小写语义、不做 Clean 归一），两份实现
  // 已经漂移，方向是**少报命中**——界面说"已排除"的那一项后端其实会处理，
  // 等于对"不会动的文件"做了假承诺。现在前端只显示这里返回的下标。
  //
  // 返回的是 **入参 paths 的下标**（升序、不重复），不是文件 ID：
  // 与 ID 解耦后，同一次调用可以服务任意候选集（勾选集、全量结果集）。
  // dirs 全空白时返回空数组 = 未启用处理策略，调用方走"不过滤"的原路径。
  filterInDirs: (dirs: string[], paths: string[]): Promise<number[]> =>
    backend().FilterInDirs(dirs, paths).then(r => r ?? []),
  // getPendingFiles 拟处理清单明细（与预览同一内核；见 BackendAPI 声明处注释）。
  getPendingFiles: (q: PendingQuery): Promise<PendingPage> => backend().GetPendingFiles(q),
  cancelOperation: (): Promise<void> => backend().CancelOperation(),
  openTrash: (): Promise<void> => backend().OpenTrash(),
  listOpRecords: (): Promise<OpRecord[]> => backend().ListOpRecords(),
  getOpRecord: (id: number): Promise<OpRecordDetail> => backend().GetOpRecord(id),
  undoOperation: (opLogId: number): Promise<string> => backend().UndoOperation(opLogId),
  undoOperationItem: (opLogId: number, itemId: number): Promise<string> => backend().UndoOperationItem(opLogId, itemId),
  clearOpRecords: (): Promise<void> => backend().ClearOpRecords(),
  cacheStats: (): Promise<CacheStats> => backend().CacheStats(),
  cacheClear: (): Promise<void> => backend().CacheClear(),
}

// 事件订阅（Wails runtime）
export function onEvent(name: string, cb: (data: any) => void): void {
  if (typeof window !== 'undefined' && window.runtime?.EventsOn) {
    window.runtime.EventsOn(name, cb)
  }
}

// 事件解绑（C12）：onUnmounted 清理，防 Vite HMR 重挂时重复绑定、toast 重复弹出
export function offEvent(name: string): void {
  const rt = typeof window !== 'undefined' ? window.runtime : undefined
  if (rt && typeof (rt as any).EventsOff === 'function') {
    ;(rt as any).EventsOff(name)
  }
}

export const isBackendAvailable = (): boolean => backendOrNull() !== null
