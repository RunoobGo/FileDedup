// 扫描任务全局状态（Pinia，M2-T03）。
import { defineStore } from 'pinia'
import { api, onEvent, isBackendAvailable } from '../wails'
import type { Filters, ProgressEvent, GroupView, FailedItem, Settings, ScanSummary, OpsProgress, OpsResult } from '../wails'
import { reactive, ref, computed } from 'vue'

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
  const resultSort = ref('reclaimable')
  const resultExt = ref('')
  const hasResult = ref(false)
  // 失败清单
  const failed = ref<FailedItem[]>([])
  const failedOpen = ref(false)
  // 预览
  const preview = ref<{ kind: string; content: string; mime?: string; path: string } | null>(null)
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
        alert(String(e))
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
      .catch((e: any) => alert('加载结果失败: ' + String(e)))
    return resultChain
  }

  async function doLoadResultPage(append: boolean) {
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
    }
    totalGroups.value = Number(r.total)
    // 全量口径（含未加载页）：统计条与 totalGroups 同源
    reclaimableTotal.value = Number(r.totalReclaimable)
  }

  function reloadResults() {
    if (!hasResult.value) return
    loadResultPage(false)
  }

  function switchView(v: 'scan' | 'result' | 'settings') {
    view.value = v
  }

  // ---------- M3：勾选 / 保留策略 / 操作 ----------

  // 组加载后重置勾选：默认勾选全部冗余项（isKeep 不可勾）。
  // 原地修改响应式 Set——整体替换会让所有已渲染卡片重算勾选状态
  function resetSelection() {
    const sel = selection.value
    sel.clear()
    for (const g of groups.value) {
      for (const f of g.files) {
        if (!f.isKeep) sel.add(f.id)
      }
    }
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
      await loadResultPage(false) // 后端决策已生效，重载视图
      resetSelection()
    } catch (e: any) {
      alert('保留策略应用失败: ' + String(e))
    }
  }

  async function clearKeep() {
    try {
      await api.clearKeepDecisions()
      await loadResultPage(false)
      resetSelection()
    } catch (e: any) {
      alert(String(e))
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
      alert(String(e))
    }
  }

  function openTrash() { api.openTrash().catch((e: any) => alert(String(e))) }

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
      await loadResultPage(false)
      resetSelection()
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
      alert('扫描失败: ' + String(e?.error ?? e))
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
      alert('保存设置失败: ' + String(e))
    }
  }

  async function openPreview(fileID: number) {
    try {
      const p = await api.previewFile(fileID)
      const f = findFile(fileID)
      preview.value = { kind: p.kind, content: p.content, mime: p.mimeType, path: f?.path ?? '' }
    } catch (e: any) {
      alert('预览失败: ' + String(e))
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
    failed, failedOpen, preview, settings, appVersion,
    selection, opsRunning, opsProgress, opsResult, currentFileID,
    previewCurrent,
    resetSelection, toggleSelect, selectAll, clearSelection, selectedFiles, selectedBytes,
    applyKeep, clearKeep, executeOp, openTrash,
    running, startScan, pauseScan, resumeScan, cancelScan,
    loadResultPage, reloadResults, switchView, bindEvents, saveSettings, applyTheme, openPreview,
    refreshStatus,
  }
})
