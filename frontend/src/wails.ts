// Wails 桥接层：类型定义 + window.go / window.runtime 包装（04 M2-T03）。
// 类型与 Go 侧 model / app.go 保持一致（01 §7 契约）。

export interface Filters {
  IncludeExts: string[]
  ExcludeExts: string[]
  MinSize: number
  MaxSize: number
  ExcludePaths: string[]
  IncludeHidden: boolean
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
  filesFailed: number
  elapsed: string
}

export interface FileView {
  id: number
  path: string
  name: string
  size: number
  mtime: number
  isKeep: boolean
}

export interface GroupView {
  groupID: number
  reclaimable: number
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

export interface OpsProgress {
  Done: number
  Total: number
  Current: string
}

export interface OpsResult {
  OK: string[]
  Failed: { Path: string; Stage: string; Err: string }[]
  Skipped: string[]
  Cancelled: string[] // P2：取消后未派发的条目（未处理，仍在结果集中）
  Reclaimed: number
}

export interface OpRequest {
  Kind: string
  FileIDs: number[]
  TargetDir?: string
  ConfirmDanger?: boolean
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
  RevealInFolder(id: number): Promise<void>
  GetSettings(): Promise<Settings>
  SaveSettings(s: Settings): Promise<Settings>
  GetVersion(): Promise<string>
  ApplyKeepPolicy(policy: { Kind: string; Directories: string[] }): Promise<KeepDecision[]>
  ClearKeepDecisions(): Promise<void>
  ExecuteOperation(op: OpRequest): Promise<string>
  CancelOperation(): Promise<void>
  OpenTrash(): Promise<void>
  CacheStats(): Promise<CacheStats>
  CacheClear(): Promise<void>
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
  applyKeepPolicy: (kind: string, dirs: string[] = []): Promise<KeepDecision[]> =>
    backend().ApplyKeepPolicy({ Kind: kind, Directories: dirs }),
  clearKeepDecisions: (): Promise<void> => backend().ClearKeepDecisions(),
  executeOperation: (op: OpRequest): Promise<string> => backend().ExecuteOperation(op),
  cancelOperation: (): Promise<void> => backend().CancelOperation(),
  openTrash: (): Promise<void> => backend().OpenTrash(),
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
