// 扫描任务全局状态（Pinia，M2-T03）。
import { defineStore } from 'pinia'
import { api, onEvent, offEvent, isBackendAvailable } from '../wails'
import type { Filters, ProgressEvent, GroupView, FailedItem, Settings, ScanSummary, OpsProgress, OpsResult, HistoryMeta, OpRecord, UndoResult } from '../wails'
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

// 大小过滤单位换算：UI 输入框标注 KB（ScanView），后端 internal/filter/filter.go
// 直接与字节比较 —— 差 1024 倍。store 内保持 KB 语义，换算只放在发往后端这一处，
// 加载/保存设置（settings.filtersDefault）走的都是 KB 口径，因此不会重复换算。
const kbToBytes = (kb: number) => (Number.isFinite(+kb) ? +kb * 1024 : 0)

export type ViewName = 'scan' | 'result' | 'settings' | 'records'

export const useScanStore = defineStore('scan', () => {
  // 导航
  const view = ref<ViewName>('scan')
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
  // v0.5.0 功能 3：扫描历史
  const histList = ref<HistoryMeta[]>([])
  const histResult = ref<HistoryMeta | null>(null) // 非空 = 当前结果集来自历史恢复（横幅提示）
  const histLoading = ref(false)
  // v0.5.0 功能 4：清理记录（回撤账本）
  const opList = ref<OpRecord[]>([])
  // 失败清单
  const failed = ref<FailedItem[]>([])
  const failedOpen = ref(false)
  // 是否处在「确认操作」对话框中（用于拦截全局快捷键，避免误改 selection 导致误删）
  const confirmOpen = ref(false)
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
  // 保留策略「指定目录」的有序优先级列表（跨页面切换保持）
  const keepDirs = ref<string[]>([])
  const opsRunning = ref(false)
  const opsProgress = ref<OpsProgress | null>(null)
  const opsResult = ref<OpsResult | null>(null)
  // 设置
  const settings = ref<Settings | null>(null)
  const appVersion = ref('')

  const running = () => ['Scanning', 'Prefiltering', 'Hashing', 'Paused'].includes(status.value)

  // B3-1：所有会改写后端状态/文件系统的入口统一过这道门。
  // 后端对同类请求本来就回绝（StartScan / ExecuteOperation / ApplyKeepPolicy /
  // Undo* / ClearOpRecords 都检 opsRunning），但只在失败时才弹一条 toast 的做
  // 法有两个问题：连点时界面先被置成"执行中"再复位（闪烁），而 clearKeep 这类
  // 入口原先后端无守卫、前端也没挡，等于在操作途中改写保护集。
  function busyReason(): string | null {
    if (opsRunning.value) return '清理/回撤操作执行中，请等待完成'
    if (scanning.value) return '扫描进行中，请等待完成'
    return null
  }
  function guard(what: string): boolean {
    const r = busyReason()
    if (r) {
      toast().notifyError(`${what}失败`, r)
      return false
    }
    return true
  }
  // 视图侧禁用态：与 guard 同源，避免"按钮可点、点了报错"。
  const busyTip = computed(() => busyReason() ?? '')
  const busy = computed(() => busyTip.value !== '')

  // B3-2：结果集代际号。后端已用同一思路弃写陈旧收尾（C7），前端必须配套：
  // 一次分页请求在途时可能启动新扫描或改开另一条历史，返回的页属于已作废的
  // 集合，照写会把旧结果盖回界面——此时界面上的 fileID 已失效，勾选即可对
  // 错文件发起清理。故请求发起时记下代际，回写前必须仍相等。
  let resultGen = 0
  function bumpResultGen() {
    resultGen++
  }

  function refreshStatus() {
    if (!isBackendAvailable()) return
    api.getStatus().then((s: string) => {
      status.value = s
      scanning.value = ['Scanning', 'Prefiltering', 'Hashing', 'Paused'].includes(s)
    })
  }

  // 上一轮结果作废：后端 StartScan 一成功就清掉 groups/byID（app.go），旧 fileID 全部失效。
  // 前端若继续显示旧结果，切到结果页既能看到旧数据、又能对失效 fileID 发起删除 →
  // 启动成功即清（启动失败时后端未清理，旧结果仍然有效，不能连带清掉）。
  // 只清结果态：roots/filters/threads/settings 是用户输入，保持不动。
  function clearStaleResult() {
    bumpResultGen() // 在途分页回包就此作废（B3-2）
    groups.value = []
    totalGroups.value = 0
    reclaimableTotal.value = 0
    resultPage.value = 0
    loadCap.value = DEFAULT_LOAD_CAP // 放量上限回到初始档
    hasResult.value = false
    histResult.value = null // 历史关联随结果集作废（新扫描会生成新历史行）
    failed.value = []
    preview.value = null
    currentFileID.value = null
    opsResult.value = null
    resetSelection()
  }

  function startScan() {
    if (roots.value.length === 0) return
    if (!guard('启动扫描')) return
    scanning.value = true
    status.value = 'Scanning'
    progress.value = null
    api
      .startScan({
        Roots: roots.value,
        // 唯一出口处做 KB → 字节换算，见文件头 kbToBytes 注释
        Filters: {
          ...filters,
          MinSize: kbToBytes(filters.MinSize),
          MaxSize: kbToBytes(filters.MaxSize),
        },
        Threads: threads.value,
        Paranoid: paranoid.value,
        UseCache: true,
      })
      .then(clearStaleResult)
      .catch((e: any) => {
        scanning.value = false
        status.value = 'Idle'
        toast().notifyError('启动扫描失败', e)
      })
  }

  // P3：后端现在对"无任务时暂停/继续/取消"返回错误（此前静默成功，
  // 按钮状态与真实状态不一致）。这里必须把失败显式提示出来，
  // 同时仍刷新状态以回到真实值——不能用 .then 让 rejection 逃逸。
  function controlScan(p: Promise<unknown>, what: string) {
    p.then(refreshStatus).catch((e: any) => {
      refreshStatus()
      toast().notifyError(`${what}失败`, e)
    })
  }
  function pauseScan() { controlScan(api.pauseScan(), '暂停') }
  function resumeScan() { controlScan(api.resumeScan(), '恢复') }
  function cancelScan() { controlScan(api.cancelScan(), '取消') }

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
    const gen = resultGen
    loadingPage.value = true
    try {
      const r = await api.getResultGroups({
        page: append ? resultPage.value : 0,
        pageSize,
        sort: resultSort.value,
        ext: resultExt.value,
      })
      if (gen !== resultGen) return // 结果集已被新扫描/新历史作废，回包不得盖回界面
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

  function switchView(v: ViewName) {
    view.value = v
  }

  // ---------- v0.5.0 功能 3：扫描历史 ----------

  async function refreshHistory() {
    if (!isBackendAvailable()) return
    try {
      histList.value = await api.listScanHistory()
    } catch {
      // 历史库不可用（如浏览器 dev 模式）：保持现有列表
    }
  }

  // openHistory 把一条历史恢复为当前结果集并切到结果页。
  // 陈旧文件安全由后端操作前逐文件校验兜底（S1/S8），载入不做文件系统遍历。
  async function openHistory(id: number) {
    const reason = busyReason()
    if (reason) {
      toast().notifyError('打开历史失败', `${reason}，请稍后再试`)
      return
    }
    histLoading.value = true
    try {
      const s = await api.loadScanHistory(id)
      bumpResultGen() // 上一代结果集就此作废（B3-2）
      groups.value = []
      resultPage.value = 0
      loadCap.value = DEFAULT_LOAD_CAP
      totalGroups.value = s.groups
      reclaimableTotal.value = s.reclaimable
      failed.value = []
      preview.value = null
      currentFileID.value = null
      opsResult.value = null
      resetSelection()
      hasResult.value = true
      histResult.value = histList.value.find((m) => m.id === id) ?? null
      await loadResultPage(false)
      view.value = 'result'
    } catch (e: any) {
      toast().notifyError('打开历史失败', e)
    } finally {
      histLoading.value = false
    }
  }

  // rescanHistory 用历史配置重新发起一次扫描。
  // 注意：history.filters 是后端原始口径（字节），store.filters 是 KB 口径
  // （startScan 出口统一 ×1024），这里回填为 KB。
  function rescanHistory(m: HistoryMeta) {
    roots.value = [...m.roots]
    Object.assign(filters, emptyFilters(), {
      IncludeExts: m.filters?.IncludeExts ?? [],
      ExcludeExts: m.filters?.ExcludeExts ?? [],
      MinSize: Math.round((m.filters?.MinSize ?? 0) / 1024),
      MaxSize: Math.round((m.filters?.MaxSize ?? 0) / 1024),
      ExcludePaths: m.filters?.ExcludePaths ?? [],
      IncludeHidden: !!m.filters?.IncludeHidden,
    })
    threads.value = m.threads
    paranoid.value = m.paranoid
    switchView('scan')
    startScan()
  }

  async function deleteHistory(id: number) {
    try {
      await api.deleteScanHistory(id)
      if (histResult.value?.id === id) histResult.value = null
      await refreshHistory()
    } catch (e: any) {
      toast().notifyError('删除历史失败', e)
    }
  }

  async function clearHistory() {
    try {
      await api.clearScanHistory()
      histResult.value = null // 当前结果集仅断开历史联动，仍可继续使用
      await refreshHistory()
    } catch (e: any) {
      toast().notifyError('清空历史失败', e)
    }
  }

  // ---------- v0.5.0 功能 4：清理记录与回撤 ----------

  async function refreshOps() {
    if (!isBackendAvailable()) return
    try {
      opList.value = await api.listOpRecords()
    } catch {
      // 历史库不可用：保持现有列表
    }
  }

  // undoRecord 回撤一条清理记录。后端同步拒绝（不可撤/在途）时走 catch；
  // 受理后的终止事件是 ops:undo:done（复用 opsRunning 互斥）。
  async function undoRecord(opId: number) {
    if (!guard('回撤')) return
    opsRunning.value = true
    opsProgress.value = { Done: 0, Total: 0, Current: '' }
    try {
      await api.undoOperation(opId)
    } catch (e: any) {
      opsRunning.value = false
      opsProgress.value = null
      toast().notifyError('回撤失败', e)
    }
  }

  // undoItem 回撤记录中的单个条目（done/undo_failed 可撤），事件与互斥同 undoRecord。
  async function undoItem(opId: number, itemId: number) {
    if (!guard('回撤')) return
    opsRunning.value = true
    opsProgress.value = { Done: 0, Total: 0, Current: '' }
    try {
      await api.undoOperationItem(opId, itemId)
    } catch (e: any) {
      opsRunning.value = false
      opsProgress.value = null
      toast().notifyError('回撤失败', e)
    }
  }

  async function clearOps() {
    if (!guard('清空清理记录')) return
    try {
      await api.clearOpRecords()
      await refreshOps()
    } catch (e: any) {
      toast().notifyError('清空清理记录失败', e)
    }
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

  // ---------- 组内全选（功能 2）：候选 = 组内全部非保留项，保留项照旧不可勾 ----------

  function groupSelCandidates(g: GroupView): number[] {
    return g.files.filter(f => !f.isKeep).map(f => f.id)
  }

  function groupSelState(g: GroupView): 'none' | 'some' | 'all' {
    const c = groupSelCandidates(g)
    if (c.length === 0) return 'none'
    const sel = selection.value
    let n = 0
    for (const id of c) if (sel.has(id)) n++
    return n === 0 ? 'none' : n === c.length ? 'all' : 'some'
  }

  function toggleGroupSelection(g: GroupView) {
    const c = groupSelCandidates(g)
    const sel = selection.value
    if (groupSelState(g) === 'all') for (const id of c) sel.delete(id)
    else for (const id of c) sel.add(id)
  }

  // addKeepDir 去重追加（Clean 意义有限，按 trim 后全等去重）
  function addKeepDir(dir: string) {
    const d = dir.trim()
    if (!d || keepDirs.value.includes(d)) return
    keepDirs.value.push(d)
  }
  function removeKeepDir(i: number) { keepDirs.value.splice(i, 1) }
  function moveKeepDir(i: number, delta: number) {
    const j = i + delta
    if (j < 0 || j >= keepDirs.value.length) return
    const [d] = keepDirs.value.splice(i, 1)
    keepDirs.value.splice(j, 0, d)
  }

  const selectedFiles = computed(() => {
    const out: { id: number; size: number; path: string }[] = []
    for (const g of groups.value)
      for (const f of g.files)
        if (selection.value.has(f.id)) out.push({ id: f.id, size: f.size, path: f.path })
    return out
  })

  const selectedBytes = computed(() => selectedFiles.value.reduce((s, f) => s + f.size, 0))

  async function applyKeep(kind: string, dirs: string[] = []) {
    if (!guard('应用保留策略')) return
    try {
      const oc = await api.applyKeepPolicy(kind, dirs)
      await loadResultPage(false) // 后端决策已生效，重载视图（并清空勾选）
      // I4：按目录保留时，组内没有任何文件落在保留目录的组不会被标出保留者，
      // 也就完全不受「保留文件不可清理」的保护——必须显式告知，不能说"已应用"就完事。
      const n = oc?.unmatchedGroups ?? 0
      if (n > 0) {
        toast().push(
          `${n} 组没有任何重复文件位于保留目录，未标出保留项、不受保护；对这些组仍需手动勾选或改用其他策略`,
          'warn',
        )
      }
    } catch (e: any) {
      toast().notifyError('保留策略应用失败', e)
    }
  }

  async function clearKeep() {
    if (!guard('重置保留决策')) return
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
    if (!guard('执行清理操作')) return
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

  // P2：清理操作可中止。语义是「停止派发后续条目」——已完成的部分不可回滚，
  // 未派发条目会出现在 OpsResult.Cancelled 中并保留在结果集里。
  function cancelOp() {
    api.cancelOperation().catch((e: any) => toast().notifyError('取消清理操作失败', e))
  }

  async function previewCurrent() {
    if (currentFileID.value != null) await openPreview(currentFileID.value)
  }

  // ---------- 事件桥绑定 ----------
  // 失败清单安全拉取：事件回调内的 rejection 无人接棒会变 unhandled。
  // 与分页同口径带代际：拉回时结果集已换代则丢弃（失败清单属于上一代结果）。
  async function refreshFailed(gen = resultGen) {
    try {
      const list = await api.getFailedItems()
      if (gen === resultGen) failed.value = list
    } catch {
      // 后端暂不可用：保留现有清单
    }
  }

  function bindEvents(): () => void {
    if (!isBackendAvailable()) return () => {}
    const bound: string[] = []
    const bind = (name: string, cb: (data: any) => void) => {
      onEvent(name, cb)
      bound.push(name)
    }
    bind('ops:progress', (p: OpsProgress) => { opsProgress.value = p })
    bind('ops:done', async (r: OpsResult) => {
      opsRunning.value = false
      opsResult.value = r
      await refreshFailed()
      await loadResultPage(false) // 重载视图并清空勾选（结果集已变化）
      refreshOps() // 清理账本已更新（FinalizeOp 在事件发出前完成）
    })
    // v0.5.0 功能 4：回撤收尾。结果集不动（恢复的文件需重扫确认），
    // 只刷新清理记录列表并给出可读的成败汇总。
    bind('ops:undo:done', async (r: UndoResult) => {
      opsRunning.value = false
      opsProgress.value = null
      if (r.failed?.length) {
        toast().notifyError('部分条目回撤失败', `成功恢复 ${r.ok} 项、失败 ${r.failed.length} 项，原因见记录明细`)
      } else if (r.ok > 0) {
        toast().notifySuccess(`回撤完成：恢复 ${r.ok} 项，建议重新扫描刷新结果`)
      } else {
        toast().push('该记录没有可回撤的条目（已回撤或未实际执行）', 'info')
      }
      await refreshOps()
    })
    // C9：后端 worker panic / 内部异常时发 ops:error（C7 守卫已捕获，不崩进程）。
    // 必须复位 opsRunning，否则操作互斥标记卡在 true → 重演 P2「操作执行中」死锁，
    // 此后所有清理/保留策略永久返回「操作执行中」，只能重启应用。
    bind('ops:error', (e: any) => {
      opsRunning.value = false
      const msg = (e && typeof e === 'object' && e.error) ? e.error : String(e ?? '未知错误')
      toast().notifyError('清理操作异常中断', msg)
    })
    // v0.5.0：后端非致命异常（如历史保存失败）→ toast 留痕，不影响当前功能
    bind('app:error', (e: any) => {
      const msg = (e && typeof e === 'object' && e.error) ? e.error : String(e ?? '未知错误')
      toast().notifyError('后台提示', msg)
    })
    // 2026-09-18 审查 C5：关窗时在途扫描/清理被拦下（后端已同时请求中止）。
    // 不给提示的话第一次点关闭看起来像"按钮坏了"。
    bind('app:quit-blocked', (e: any) => {
      const msg = (e && typeof e === 'object' && e.message) ? e.message : String(e ?? '任务进行中')
      toast().push(msg, 'info')
    })
    bind('scan:progress', (ev: ProgressEvent) => {
      progress.value = ev
    })
    bind('scan:stage', (ev: { Stage: string; Desc: string }) => {
      stageDesc.value = ev.Desc || ev.Stage
      refreshStatus()
    })
    bind('scan:done', async (s: ScanSummary) => {
      scanning.value = false
      status.value = 'Done'
      reclaimableTotal.value = s.reclaimable // 初始值，随即被 loadResultPage 全量口径覆盖
      hasResult.value = true
      await refreshFailed()
      await loadResultPage(false)
      view.value = 'result' // 完成后自动切结果页（M2-T05）
    })
    bind('scan:cancelled', async () => {
      scanning.value = false
      status.value = 'Cancelled'
      await refreshFailed()
    })
    bind('scan:error', (e: any) => {
      scanning.value = false
      status.value = 'Failed'
      toast().notifyError('扫描失败', e)
    })
    bind('app:ready', (v: string) => {
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
    // C12：返回解绑函数——HMR/卸载时清掉全部订阅，防重复绑定
    return () => { for (const n of bound) offEvent(n) }
  }

  function applyTheme(theme: string) {
    const dark =
      theme === 'dark' ||
      (theme === 'system' &&
        window.matchMedia('(prefers-color-scheme: dark)').matches)
    document.documentElement.dataset.theme = dark ? 'dark' : 'light'
  }

  // B3-3：保存失败必须让调用方知道。修正前这里把异常吞成 fulfilled 的
  // undefined，设置页 `store.settings = await store.saveSettings(draft)`
  // 于是把整个设置态赋成 undefined，同时还提示"已保存"。
  // 主题按后端回包（已归一）应用，不按草稿应用，避免界面与实际生效值分叉。
  async function saveSettings(s: Settings): Promise<Settings> {
    const saved = await api.saveSettings(s)
    settings.value = saved
    applyTheme(saved.theme)
    return saved
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
    histList, histResult, histLoading, refreshHistory, openHistory, rescanHistory, deleteHistory, clearHistory,
    opList, refreshOps, undoRecord, undoItem, clearOps,
    failed, failedOpen, confirmOpen, preview, settings, appVersion,
    selection, opsRunning, opsProgress, opsResult, currentFileID,
    busy, busyTip,
    keepDirs, addKeepDir, removeKeepDir, moveKeepDir,
    previewCurrent,
    resetSelection, toggleSelect, selectAll, clearSelection, selectedFiles, selectedBytes,
    groupSelState, toggleGroupSelection,
    applyKeep, clearKeep, executeOp, openTrash, cancelOp,
    running, startScan, pauseScan, resumeScan, cancelScan,
    loadResultPage, loadMore, reloadResults, switchView, bindEvents, saveSettings, applyTheme, openPreview,
    revealPreview,
    refreshStatus,
  }
})
