// 扫描任务全局状态（Pinia，M2-T03）。
import { defineStore } from 'pinia'
import { api, onEvent, isBackendAvailable } from '../wails'
import type { Filters, ProgressEvent, GroupView, FailedItem, Settings, ScanSummary, OpsProgress, OpsResult } from '../wails'
import { reactive, ref, computed } from 'vue'
import { useToastStore } from './toast'

// 错误提示统一走 toast（Y6：替代阻塞式 alert，且可堆叠查看多条）
const toast = () => useToastStore()

export const emptyFilters = (): Filters => ({
  IncludeExts: [],
  ExcludeExts: [],
  MinSize: 0,
  MaxSize: 0,
  ExcludePaths: [],
  IncludeHidden: false,
})

export const useScanStore = defineStore('scan', () => {
  // 导航
  const view = ref<'scan' | 'result' | 'settings'>('scan')
  // 扫描配置
  const roots = ref<string[]>([])
  const filters = reactive<Filters>(emptyFilters())
  const threads = ref(0)
  const paranoid = ref(false)
  // 运行态
  const status = ref('Idle')
  const progress = ref<ProgressEvent | null>(null)
  const stageDesc = ref('')
  const scanning = ref(false)
  // 结果
  const groups = ref<GroupView[]>([])
  const totalGroups = ref(0)
  const reclaimableTotal = ref(0)
  const resultPage = ref(0)
  const pageSize = 100
  // Y7：结果页 DOM 放量上限——无限滚动最多累积 DEFAULT_LOAD_CAP 组，
  // 之后需显式「继续加载」再放量一档。渲染层有 content-visibility 兜底，
  // 但万级组仍会让组件实例与 DOM 节点无界增长（内存无法回收）。
  const DEFAULT_LOAD_CAP = 2000
  const LOAD_CAP_STEP = 1000
  const loadCap = ref(DEFAULT_LOAD_CAP)
  const loadingPage = ref(false) // 在途请求标志：防快速滚动重复排队拉取
  const resultSort = ref('reclaimable')
  const resultExt = ref('')
  const hasResult = ref(false)
  // 失败清单
  const failed = ref<FailedItem[]>([])
  const failedOpen = ref(false)
  // 预览
  // P2-7：预览面板原先只带 path，头部无法回答"这是什么类型/多大/什么时候改的"，
  // 也没有回到磁盘定位的入口。这里把 FileView 已有的元信息一并带上
  // （fileID 用于 RevealInFolder，其余供头部元信息行展示）。
  const preview = ref<{
    fileID: number
    kind: string
    content: string
    mime?: string
    path: string
    name: string
    size: number
    mtime: number
  } | null>(null)
  const currentFileID = ref<number | null>(null) // Space 快捷键预览目标
  // 勾选与操作（M3）
  const selection = ref<Set<number>>(new Set())
  const opsRunning = ref(false)
  const opsProgress = ref<OpsProgress | null>(null)
  const opsResult = ref<OpsResult | null>(null)
  // 设置
  const settings = ref<Settings | null>(null)
  const appVersion = ref('')

  const running = () => ['Scanning', 'Prefiltering', 'Hashing', 'Paused'].includes(status.value)

  function refreshStatus() {
    if (!isBackendAvailable()) return
    api.getStatus().then((s: string) => {
      status.value = s
      scanning.value = ['Scanning', 'Prefiltering', 'Hashing', 'Paused'].includes(s)
    })
  }

  function startScan() {
    if (roots.value.length === 0) return
    scanning.value = true
    status.value = 'Scanning'
    progress.value = null
    api
      .startScan({
        Roots: roots.value,
        Filters: { ...filters },
        Threads: threads.value,
        Paranoid: paranoid.value,
        UseCache: true,
      })
      .catch((e: any) => {
        scanning.value = false
        status.value = 'Idle'
        toast().notifyError('启动扫描失败', e)
      })
  }

  function pauseScan() { api.pauseScan().then(refreshStatus) }
  function resumeScan() { api.resumeScan().then(refreshStatus) }
  function cancelScan() { api.cancelScan().then(refreshStatus) }

  // 结果页加载串行化：滚动加载/排序/筛选并发触发时按序执行，
  // 避免同页重复请求（:key 冲突）与新旧响应交错覆盖
  let resultChain: Promise<void> = Promise.resolve()
  function loadResultPage(append: boolean): Promise<void> {
    resultChain = resultChain
      .then(() => doLoadResultPage(append))
      .catch((e: any) => toast().notifyError('加载结果失败', e))
    return resultChain
  }

  async function doLoadResultPage(append: boolean) {
    // Y7：已达放量上限时忽略滚动追加，需经 loadMore 显式放量
    if (append && groups.value.length >= loadCap.value) return
    loadingPage.value = true
    try {
      const r = await api.getResultGroups({
        page: append ? resultPage.value : 0,
        pageSize,
        sort: resultSort.value,
        ext: resultExt.value,
      })
      if (append) {
        groups.value.push(...r.groups)
        resultPage.value++
      } else {
        groups.value = r.groups
        resultPage.value = 1
        // Y8：列表被替换（排序/筛选/新扫描/操作后刷新）时勾选已无意义 → 清空
        resetSelection()
      }
      totalGroups.value = Number(r.total)
      // 全量口径（含未加载页）：统计条与 totalGroups 同源
      reclaimableTotal.value = Number(r.totalReclaimable)
    } finally {
      loadingPage.value = false
    }
  }

  // loadMore 放量一档并加载下一页（Y7：显式继续加载，替代无界滚动）
  function loadMore(): Promise<void> {
    if (groups.value.length >= loadCap.value) loadCap.value += LOAD_CAP_STEP
    return loadResultPage(true)
  }

  function reloadResults() {
    if (!hasResult.value) return
    loadResultPage(false)
  }

  function switchView(v: 'scan' | 'result' | 'settings') {
    view.value = v
  }

  // ---------- M3：勾选 / 保留策略 / 操作 ----------

  // resetSelection 清空勾选（Y8）。
  // 修正前语义为「默认勾选全部冗余项」：万级结果下与常驻的「永久删除」按钮
  // 组合，一次误点仅隔一个确认框，复核成本过高 → 改为默认不勾选 + 界面引导，
  // 由用户显式「全选」或逐项勾选（保留项始终不可勾）。
  function resetSelection() {
    selection.value.clear()
  }

  function toggleSelect(id: number) {
    const sel = selection.value
    if (sel.has(id)) sel.delete(id)
    else sel.add(id)
  }

  function selectAll() {
    const sel = selection.value
    sel.clear()
    for (const g of groups.value) for (const f of g.files) if (!f.isKeep) sel.add(f.id)
  }

  function clearSelection() {
    selection.value.clear()
  }

  const selectedFiles = computed(() => {
    const out: { id: number; size: number; path: string }[] = []
    for (const g of groups.value)
      for (const f of g.files)
        if (selection.value.has(f.id)) out.push({ id: f.id, size: f.size, path: f.path })
    return out
  })

  const selectedBytes = computed(() => selectedFiles.value.reduce((s, f) => s + f.size, 0))

  async function applyKeep(kind: string, directory?: string) {
    try {
      await api.applyKeepPolicy(kind, directory)
      await loadResultPage(false) // 后端决策已生效，重载视图（并清空勾选）
    } catch (e: any) {
      toast().notifyError('保留策略应用失败', e)
    }
  }

  async function clearKeep() {
    try {
      await api.clearKeepDecisions()
      await loadResultPage(false)
    } catch (e: any) {
      toast().notifyError('重置保留决策失败', e)
    }
  }

  async function executeOp(kind: 'trash' | 'delete' | 'move' | 'hardlink', targetDir?: string, confirmDanger = false) {
    const ids = selectedFiles.value.map(f => f.id)
    if (ids.length === 0) return
    opsRunning.value = true
    opsResult.value = null
    opsProgress.value = { Done: 0, Total: ids.length, Current: '' }
    try {
      await api.executeOperation({
        Kind: kind, FileIDs: ids, TargetDir: targetDir ?? '', ConfirmDanger: confirmDanger,
      })
    } catch (e: any) {
      opsRunning.value = false
      toast().notifyError('执行清理操作失败', e)
    }
  }

  function openTrash() { api.openTrash().catch((e: any) => toast().notifyError('打开回收站失败', e)) }

  async function previewCurrent() {
    if (currentFileID.value != null) await openPreview(currentFileID.value)
  }

  // ---------- 事件桥绑定 ----------
  // 失败清单安全拉取：事件回调内的 rejection 无人接棒会变 unhandled
  async function refreshFailed() {
    try {
      failed.value = await api.getFailedItems()
    } catch {
      // 后端暂不可用：保留现有清单
    }
  }

  function bindEvents() {
    if (!isBackendAvailable()) return
    onEvent('ops:progress', (p: OpsProgress) => { opsProgress.value = p })
    onEvent('ops:done', async (r: OpsResult) => {
      opsRunning.value = false
      opsResult.value = r
      await refreshFailed()
      await loadResultPage(false) // 重载视图并清空勾选（结果集已变化）
    })
    onEvent('scan:progress', (ev: ProgressEvent) => {
      progress.value = ev
    })
    onEvent('scan:stage', (ev: { Stage: string; Desc: string }) => {
      stageDesc.value = ev.Desc || ev.Stage
      refreshStatus()
    })
    onEvent('scan:done', async (s: ScanSummary) => {
      scanning.value = false
      status.value = 'Done'
      reclaimableTotal.value = s.reclaimable // 初始值，随即被 loadResultPage 全量口径覆盖
      hasResult.value = true
      await refreshFailed()
      await loadResultPage(false)
      view.value = 'result' // 完成后自动切结果页（M2-T05）
    })
    onEvent('scan:cancelled', async () => {
      scanning.value = false
      status.value = 'Cancelled'
      await refreshFailed()
    })
    onEvent('scan:error', (e: any) => {
      scanning.value = false
      status.value = 'Failed'
      toast().notifyError('扫描失败', e)
    })
    onEvent('app:ready', (v: string) => {
      appVersion.value = v
      refreshStatus()
    })
    // 初始拉取
    api.getVersion().then((v: string) => (appVersion.value = v)).catch(() => {})
    api.getSettings().then((s: Settings) => {
      settings.value = s
      // 应用主题
      applyTheme(s.theme)
    }).catch(() => {})
    refreshStatus()
  }

  function applyTheme(theme: string) {
    const dark =
      theme === 'dark' ||
      (theme === 'system' &&
        window.matchMedia('(prefers-color-scheme: dark)').matches)
    document.documentElement.dataset.theme = dark ? 'dark' : 'light'
  }

  async function saveSettings(s: Settings) {
    try {
      settings.value = await api.saveSettings(s)
      applyTheme(s.theme)
    } catch (e: any) {
      toast().notifyError('保存设置失败', e)
    }
  }

  async function openPreview(fileID: number) {
    try {
      const p = await api.previewFile(fileID)
      const f = findFile(fileID)
      preview.value = {
        fileID,
        kind: p.kind,
        content: p.content,
        mime: p.mimeType,
        path: f?.path ?? '',
        name: f?.name ?? '',
        size: f?.size ?? 0,
        mtime: f?.mtime ?? 0,
      }
    } catch (e: any) {
      toast().notifyError('预览失败', e)
    }
  }

  // P2-7：从预览面板直接定位到所在文件夹（复用结果行上的同一后端能力）
  async function revealPreview() {
    const id = preview.value?.fileID
    if (id == null) return
    try {
      await api.revealInFolder(id)
    } catch (e: any) {
      toast().notifyError('打开所在文件夹失败', e)
    }
  }

  function findFile(id: number) {
    for (const g of groups.value) {
      const f = g.files.find((x) => x.id === id)
      if (f) return f
    }
    return null
  }

  return {
    view, roots, filters, threads, paranoid,
    status, progress, stageDesc, scanning,
    groups, totalGroups, reclaimableTotal, resultSort, resultExt, hasResult, pageSize,
    loadCap, loadingPage, resultPage,
    failed, failedOpen, preview, settings, appVersion,
    selection, opsRunning, opsProgress, opsResult, currentFileID,
    previewCurrent,
    resetSelection, toggleSelect, selectAll, clearSelection, selectedFiles, selectedBytes,
    applyKeep, clearKeep, executeOp, openTrash,
    running, startScan, pauseScan, resumeScan, cancelScan,
    loadResultPage, loadMore, reloadResults, switchView, bindEvents, saveSettings, applyTheme, openPreview,
    revealPreview,
    refreshStatus,
  }
})
