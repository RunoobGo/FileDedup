<script setup lang="ts">
// 记录页（v0.5.0 功能 3+4）：扫描历史列表 + 恢复/重扫/删除；清理记录 + 回撤。
import { computed, onMounted, ref, watch } from 'vue'
import { useScanStore } from '../stores/scan'
import { useToastStore } from '../stores/toast'
import { api } from '../wails'
import { humanBytes, formatCount, formatUnixSec } from '../utils/format'
import { undoBlockedTitle, undoTitleCodeForKind } from '../utils/undoReason'
// M290/M297（2026-09-27 实施批）：软链接合并的"数据在哪儿、怎么拿回来"这一族
// 判据与文案，全仓只写在 utils/keepsource.ts（M79 同一纪律：视图不内联整句、不自拼判据）。
import {
  KEEP_SOURCE_LABEL,
  danglingKeepTitle,
  keepSourceTitle,
  offersInAppUndo,
  recordedDest,
  revealKeepTitle,
} from '../utils/keepsource'
import Icon from '../components/Icon.vue'
import type { HistoryMeta, OpRecord, OpRecordItem } from '../wails'

const store = useScanStore()
const toast = useToastStore()
const tab = ref<'scans' | 'ops'>('scans')
// ConfirmDialog 是清理操作专用（勾选数/移动目标耦合），历史/记录的确认用两步式行内确认
const confirmClear = ref(false)

onMounted(() => {
  store.refreshHistory()
  store.refreshOps()
})
watch(tab, (t) => { if (t === 'ops') store.refreshOps() })

const loading = computed(() => store.histLoading)

function rootsSummary(m: HistoryMeta): string {
  if (!m.roots.length) return '—'
  return m.roots.length === 1 ? m.roots[0] : `${m.roots[0]} 等 ${m.roots.length} 项`
}

// 恢复后横幅需要区分「已清理 n」：files 为当前存量，origFiles 为保存时数量
function cleanedCount(m: HistoryMeta): number {
  return Math.max(0, m.origFiles - m.files)
}

function open(m: HistoryMeta) { store.openHistory(m.id) }
function rescan(m: HistoryMeta) { store.rescanHistory(m) }
function del(m: HistoryMeta) { store.deleteHistory(m.id) }

async function clearAll() {
  if (!confirmClear.value) { confirmClear.value = true; return }
  confirmClear.value = false
  await store.clearHistory()
}

// ---------- 清理记录（功能 4） ----------

const OP_KIND_LABEL: Record<string, string> = {
  trash: '回收站', delete: '永久删除', move: '移动',
  hardlink: '硬链接合并', symlink: '软链接合并',
}
const STATE_LABEL: Record<string, string> = {
  planned: '待处理', done: '已执行', failed: '失败', skipped: '已跳过',
  cancelled: '已取消', interrupted: '中断', undone: '已回撤', undo_failed: '回撤失败',
  undoing: '回撤中',
}

// done 口径含已回撤项（执行账本不冲销），剩余可撤 = done - undone
function undoLeft(m: OpRecord): number {
  return m.undoable ? Math.max(0, m.done - m.undone) : 0
}

// noUndoTitle 「不可回撤」徽标的悬浮说明。这两句中文不在本文件——M79（裁定"判据归
// 后端、文案归前端"）之后全仓只有一处写它（`utils/undoReason.ts`），这里只把记录映成
// 原因码再问它。为什么这一侧不必问平台：徽标只在 `m.undoable === false` 时渲染（见模板），
// 而 `undoable` 是后端按 `undoableFor(kind, GOOS)` 算好下传的 ⇒ 非 Windows 的 trash
// 根本走不到"回收站那一码"，判据仍然只有一份。
function noUndoTitle(m: OpRecord): string {
  return undoBlockedTitle(undoTitleCodeForKind(m.kind))
}

function stateCls(s: string): string {
  if (s === 'undone' || s === 'done') return s === 'undone' ? 'st st-ok' : 'st st-done'
  if (s === 'failed' || s === 'undo_failed' || s === 'interrupted') return 'st st-bad'
  return 'st st-mute'
}

const confirmUndoId = ref<number | null>(null)
const confirmClearOps = ref(false)
// M365：清空清理记录的在途标记（形状同本页的 exporting/importing，也同 scan store 的 busy）。
const clearingOps = ref(false)
const expandedOp = ref<number | null>(null)
const opDetail = ref<OpRecordItem[] | null>(null)
const opDetailLoading = ref(false)

async function toggleOpDetail(m: OpRecord) {
  if (expandedOp.value === m.id) {
    expandedOp.value = null
    opDetail.value = null
    return
  }
  expandedOp.value = m.id
  opDetail.value = null
  opDetailLoading.value = true
  try {
    const d = await api.getOpRecord(m.id)
    opDetail.value = d.list ?? []
  } catch (e: any) {
    toast.notifyError('读取记录明细失败', e)
    expandedOp.value = null
  } finally {
    opDetailLoading.value = false
  }
}

function askUndo(m: OpRecord) {
  if (confirmUndoId.value === m.id) {
    confirmUndoId.value = null
    store.undoRecord(m.id)
  } else {
    confirmUndoId.value = m.id
  }
}

// ---------- 单项回撤 ----------

const undoingItem = ref<number | null>(null)

// done 可撤；undo_failed 给修正后重试通道（与批量回撤的后端口径一致）。
// ★ 第三道门 offersInAppUndo 是本轮加的：软链接合并摆不出"应用内回撤"这个出口
//   （M290 真机定案——合并成功即无条件删备份，而回撤以"备份必须在"为硬前提）。
//   判据只写在 utils/keepsource.ts 一处，视图只引用（M79 同一纪律）。
function canUndoItem(m: OpRecord, it: OpRecordItem): boolean {
  return m.undoable && offersInAppUndo(m.kind) &&
    (it.state === 'done' || it.state === 'undo_failed')
}

// revealKeep 明细行的「直达保留原目录」——撤下回撤按钮后，用户唯一的找回路径。
//
// ★ 刻意不做前端预检（"linkSrc 看着不存在就把按钮藏起来"）：目标不存在恰恰是
//   最需要这个按钮的时候，而"没了就打开它所在的目录"是后端 RevealKeepSource 的
//   判据（AS-H6 / 功能 4 同一纪律：前端拦下来只会把出口变成"没点可点"）。
function revealKeep(it: OpRecordItem) {
  const src = it.linkSrc
  if (!src) return
  api.revealKeepSource(src).catch((e: any) => toast.notifyError('打开保留源所在目录失败', e))
}

async function undoOne(m: OpRecord, it: OpRecordItem) {
  undoingItem.value = it.id
  // ★ 必须先问"受理了没有"再决定留不留这个"执行中…"（R3-4）：guard 拒掉那一次
  //   opsRunning 根本没置位，下面那个下降沿 watch 就永远不会来清，
  //   这一行会永久挂着"执行中…"，而它看上去像一个还在跑的任务。
  const accepted = await store.undoItem(m.id, it.id)
  if (!accepted) undoingItem.value = null
}

async function reloadDetail() {
  if (expandedOp.value === null) return
  try {
    const d = await api.getOpRecord(expandedOp.value)
    opDetail.value = d.list ?? []
  } catch {
    // 明细刷新失败不打断：列表计数已由 refreshOps 更新
  }
}

// 回撤收尾（opsRunning 下降沿）：清执行中标记并重拉展开中的明细
watch(() => store.opsRunning, (now, prev) => {
  if (prev && !now) {
    undoingItem.value = null
    reloadDetail()
  }
})

async function clearAllOps() {
  if (!confirmClearOps.value) { confirmClearOps.value = true; return }
  // M365（第八轮批 2）：store.busy 只含"扫描/清理在途"这一档，不含"正在清记录"，
  // 而改前确认态写在 await **之前** ⇒ 请求还在路上入口钮就复活，连点会并发发出
  // 第二次 ClearOpRecords，正好撞在本批后端补的那道闸上。落定之后才关。
  if (clearingOps.value) return
  clearingOps.value = true
  try {
    await store.clearOps()
  } finally {
    confirmClearOps.value = false
    clearingOps.value = false
  }
}

// ---------- 记录导出 / 导入（M351/M352/M353，裁定 R-2） ----------
//
// 这一对按钮是本仓"防误删"唯一有恢复力的出口：清缓存那格的 cache-backup.db 只保哈希
// 缓存（本就可重算），账本不可再生。导出的意义全在"以后还能导回来"，所以回执上的
// 每个数字都要报给用户——只说"导出成功"等于什么都没证实。
const exporting = ref(false)
const importing = ref(false)
const confirmImport = ref(false)

async function exportRecords() {
  exporting.value = true
  try {
    const res = await api.exportRecords()
    if (res.cancelled) return // 取消不是失败，弹任何 toast 都是 noise
    toast.notifySuccess(
      `已导出记录：扫描历史 ${formatCount(res.scans)} 条 · 清理记录 ${formatCount(res.ops)} 条 · ` +
      `${humanBytes(res.bytes)}（影像 ${res.dbPath}）`
    )
  } catch (e: any) {
    toast.notifyError('导出记录失败', e)
  } finally {
    exporting.value = false
  }
}

// 导入是两步式就地确认（与本页两个「清空」同一形状，不用原生 confirm()）。
// 为什么导入也要确认：它会把外来文件的路径写进本地账本，而账本里的 dest_path/link_src
// 在明细展示与「直达保留原目录」那条腿上是**被信任**的（设计段 §6.4 记了这条让步）。
async function importRecords() {
  if (!confirmImport.value) { confirmImport.value = true; return }
  confirmImport.value = false
  importing.value = true
  try {
    const res = await api.importRecords()
    if (res.cancelled) return
    toast.notifySuccess(
      `已导入记录：新增扫描 ${formatCount(res.scansAdded)} 条（跳过重复 ${formatCount(res.scansSkipped)}）· ` +
      `新增清理记录 ${formatCount(res.opsAdded)} 条（跳过重复 ${formatCount(res.opsSkipped)}）` +
      (res.opsOrphaned > 0 ? ` · ${formatCount(res.opsOrphaned)} 条未能关联到本地扫描历史` : '')
    )
    // 两个列表都要刷：导入同时新增扫描历史与清理记录，只刷当前 tab 会让另一侧停留在旧读数。
    await Promise.all([store.refreshHistory(), store.refreshOps()])
  } catch (e: any) {
    toast.notifyError('导入记录失败', e)
  } finally {
    importing.value = false
  }
}

// 明细表「去向」列：trash/move 看 destPath，链接类（hardlink/symlink）看 linkSrc
// destSummary 条目「去向」列的文案。
//
// 链接类条目的"去向"是**记账那一刻**写下的 linkSrc，不是现读的链接指向 ⇒ 文案必须自带
// 这个口径（M291：真机上把链接改指到第三方文件后，这一行仍显示 `软链接 → …\keep.bin`，
// 台账看起来完全健康，而执行侧的 verifySymlinked 当场就拦了——两句口径不一致）。
// 造句住在 utils/keepsource.ts，视图只引用（M79 同一纪律）。
// 两档仍按后端 isSymlink 分开标：那个标记来自 Lstat，比按 OP_KIND_LABEL 猜可靠（M290）。
function destSummary(it: OpRecordItem): string {
  if (it.destPath) return it.destPath
  if (it.linkSrc) return recordedDest(it.linkSrc, !!it.isSymlink)
  return '—'
}

// 悬空链接行的说明不住本文件（danglingKeepTitle，utils/keepsource）。
// ★ 这里刻意**不复述**改前那两句 title 的原话：它们对用户的保证与盘上实况相反，
//   而 frontend/tests/records-keepsource.test.ts 把那两个形状钉成"视图里不得再现"，
//   注释里抄一遍就会把那条锚撞红（真机读数在 04 §6.50 一 W3-5，要看原话去那里）。
</script>

<template>
  <div class="records-view">
    <div class="tabs panel">
      <button :class="{ on: tab === 'scans' }" @click="tab = 'scans'">扫描历史</button>
      <button :class="{ on: tab === 'ops' }" @click="tab = 'ops'">清理记录</button>
      <span class="spacer"></span>
      <!-- M353：导出/导入两个入口常驻（不随 tab 隐藏）——账本是两张表，任一表的恢复
           都需要成对导出，藏在 tab 里会让人以为"这个 tab 才导这个 tab 的记录"。 -->
      <button class="btn-ghost" :disabled="store.busy || exporting || importing"
        :title="store.busyTip || '导出记录影像（.db，可导回）与只读镜像（.json，给人核对）各一份'"
        @click="exportRecords">{{ exporting ? '导出中…' : '导出记录' }}</button>
      <button v-if="!confirmImport" class="btn-ghost" :disabled="store.busy || exporting || importing"
        :title="store.busyTip || '从影像增量并入本地记录：只新增本地没有的行，不删除、不改写已有记录'"
        @click="importRecords">{{ importing ? '导入中…' : '导入记录' }}</button>
      <template v-else>
        <span class="confirm-tip">确认导入所选记录影像？只新增本地没有的记录，不会删除或改写已有记录</span>
        <button class="btn-primary" :disabled="store.busy || exporting || importing" @click="importRecords">确认导入</button>
        <button class="btn-ghost" @click="confirmImport = false">取消</button>
      </template>
      <!-- M296（2026-09-27 实施批）：改前是 `v-if="… store.opsRunning"` 配一句写死的
           「回撤执行中…」⇒ busy 的三个原因里只说得出一个，扫描中/历史载入中整页按钮
           全灰却没有一句解释。文案直接取 store.busyTip（busyReason 的唯一派生出口），
           这也是 M286 那条取向的落地：把"为什么是灰的"摆成**可见文本**，
           而不是只挂在 title 上等 OS 绘制（M293 证过那层浮窗合成不出来）。 -->
      <span v-if="store.busy" class="undo-hint">{{ store.busyTip }}</span>
      <template v-if="tab === 'scans' && store.histList.length">
        <button v-if="!confirmClear" class="btn-ghost" @click="confirmClear = true">清空</button>
        <template v-else>
          <span class="confirm-tip">确认清空全部历史？（不影响当前结果集）</span>
          <button class="btn-danger" @click="clearAll">确认清空</button>
          <button class="btn-ghost" @click="confirmClear = false">取消</button>
        </template>
      </template>
      <template v-if="tab === 'ops'">
        <button class="btn-ghost" title="在系统回收站中查看/还原已回收文件" @click="store.openTrash()">打开系统回收站</button>
        <template v-if="store.opList.length">
          <!-- B3-1：账本在途时不可清空（后端同样拒绝，这里免掉"点了才报错"）。
               M365：store.busy 不含"正在清记录"这一档，所以两枚按钮各自再挂在 clearingOps 上。 -->
          <button v-if="!confirmClearOps" class="btn-ghost" :disabled="store.busy || clearingOps"
            :title="store.busyTip || '清空全部清理记录'" @click="confirmClearOps = true">清空</button>
          <template v-else>
            <span class="confirm-tip">确认清空清理记录？清空后未回撤的操作将无法再回撤</span>
            <button class="btn-danger" :disabled="store.busy || clearingOps" @click="clearAllOps">确认清空</button>
            <button class="btn-ghost" @click="confirmClearOps = false">取消</button>
          </template>
        </template>
      </template>
    </div>

    <!-- 标签一：扫描历史 -->
    <div v-if="tab === 'scans'" class="body">
      <div v-if="!store.histList.length" class="empty panel">
        <Icon class="empty-ico" name="history" :size="32" :stroke="1.7" />
        <div class="empty-title">暂无扫描历史</div>
        <p class="empty-desc">完成一次扫描后会自动保存到此处（最多 20 条），可随时恢复继续清理。</p>
        <div class="empty-actions">
          <button class="btn-primary" @click="store.switchView('scan')">去扫描</button>
        </div>
      </div>
      <table v-else class="panel hist-table">
        <thead>
          <tr>
            <th>保存时间</th><th>扫描目录</th><th>重复组</th><th>文件数</th>
            <th>可释放</th><th class="ops-col">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="m in store.histList" :key="m.id" :class="{ current: store.histResult?.id === m.id }">
            <td class="mono">{{ formatUnixSec(m.savedAt) }}</td>
            <td class="roots" :title="m.roots.join('\n')">{{ rootsSummary(m) }}</td>
            <td class="num">{{ formatCount(m.groups) }}</td>
            <td class="num">
              {{ formatCount(m.files) }}
              <span v-if="cleanedCount(m)" class="cleaned">已清理 {{ formatCount(cleanedCount(m)) }}</span>
            </td>
            <td class="num">{{ humanBytes(m.reclaimable) }}</td>
            <td class="ops-col">
              <!-- FE-3（2026-09-21 全量审查）：禁用态只问 store.histBusy。
                   原先这三个按钮各自拼表达式，且拼的是**两份不一致**的判据：
                   恢复漏了 histLoading 之外的写法、重扫漏了 histLoading、删除什么都没挡。
                   openHistory 期间界面仍停在记录页（视图切换在回包之后），
                   于是"载入中"照样能点重扫/删除——新扫描与载入回包同时写同一批状态。 -->
              <!-- M292（2026-09-27 实施批）：这一格的 :title 原先自拼一套
                   scanning → opsRunning → loading 的优先级，与 store.busyReason() 的顺序
                   **恰好相反**（那边 opsRunning 排第一），于是两态同真时"为什么是灰的"
                   在两处给出两个句子——真机是三份逐采样日志读出来的（04 §6.50 一）。
                   现在灰了就照读 busyReason 的那一句，不灰才说这个按钮干什么。 -->
              <button class="btn-primary" :disabled="store.histBusy"
                :title="store.busyTip || '恢复该结果并可继续清理'"
                @click="open(m)">恢复</button>
              <!-- M296 同一把尺子量到底：这两颗也吃 histBusy，静态 title 只说"干什么"、
                   不说"为什么灰"⇒ 灰着时同样是一句假解释。 -->
              <button class="btn-ghost" :disabled="store.histBusy"
                :title="store.busyTip || '按此配置重新扫描'" @click="rescan(m)">重扫</button>
              <button class="btn-ghost del" :disabled="store.histBusy"
                :title="store.busyTip || '删除该条历史'" @click="del(m)">删除</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 标签二：清理记录（写前账本 + 回撤） -->
    <div v-else class="body">
      <div v-if="!store.opList.length" class="empty panel">
        <Icon class="empty-ico" name="undo" :size="32" :stroke="1.7" />
        <div class="empty-title">暂无清理记录</div>
        <p class="empty-desc">清理操作会自动留痕；多数操作支持回撤，能不能撤以每条记录的徽标为准。</p>
      </div>
      <table v-else class="panel hist-table">
        <thead>
          <tr>
            <th>时间</th><th>类型</th><th>条数</th><th>已回撤</th>
            <th>回收空间</th><th class="ops-col">操作</th>
          </tr>
        </thead>
        <tbody>
          <template v-for="m in store.opList" :key="m.id">
            <tr :class="{ expanded: expandedOp === m.id }">
              <td class="mono">{{ formatUnixSec(m.createdAt) }}</td>
              <td>
                <span class="kind-badge" :class="'k-' + m.kind">{{ OP_KIND_LABEL[m.kind] ?? m.kind }}</span>
                <span v-if="!m.undoable" class="no-undo" :title="noUndoTitle(m)">不可回撤</span>
                <!-- 软链接合并的账本写的是 undoable=true（后端事实，本轮未改），但那条腿
                     真机走不到 ⇒ 这一格不摆「回撤」，改摆用户此刻能做的事（M290/M297）。 -->
                <span v-if="!offersInAppUndo(m.kind)" class="keep-hint" :title="keepSourceTitle()">
                  {{ KEEP_SOURCE_LABEL }}
                </span>
              </td>
              <td class="num">
                {{ formatCount(m.items) }}
                <span v-if="m.failed" class="cleaned">失败 {{ m.failed }}</span>
              </td>
              <td class="num">{{ m.undone ? formatCount(m.undone) : '—' }}</td>
              <td class="num">{{ humanBytes(m.reclaimable) }}</td>
              <td class="ops-col">
                <button v-if="offersInAppUndo(m.kind) && undoLeft(m) > 0" class="btn-primary"
                  :disabled="store.busy"
                  :title="store.busyTip || `恢复 ${undoLeft(m)} 项文件`"
                  @click="askUndo(m)">
                  {{ confirmUndoId === m.id ? `确认回撤 ${undoLeft(m)} 项` : '回撤' }}
                </button>
                <button v-else-if="offersInAppUndo(m.kind) && m.undoable && m.done" class="btn-ghost" disabled>已回撤</button>
                <button v-if="confirmUndoId === m.id" class="btn-ghost"
                  @click="confirmUndoId = null">取消</button>
                <button class="btn-ghost" @click="toggleOpDetail(m)">
                  {{ expandedOp === m.id ? '收起' : '明细' }}
                </button>
              </td>
            </tr>
            <tr v-if="expandedOp === m.id" class="detail-row">
              <td colspan="6">
                <div v-if="opDetailLoading" class="detail-loading">读取明细…</div>
                <table v-else-if="opDetail" class="item-table">
                  <thead>
                    <tr><th>原路径</th><th>去向</th><th>大小</th><th>状态</th><th>错误</th><th>操作</th></tr>
                  </thead>
                  <tbody>
                    <tr v-for="it in opDetail" :key="it.id" :class="{ 'row-dangling': it.dangling }">
                      <td class="roots" :title="it.origPath">{{ it.origPath }}</td>
                      <td class="roots" :title="destSummary(it)">
                        {{ destSummary(it) }}
                        <!-- 悬空链接必须一眼可见（红标 + 可悬停看原因）。
                             不做自动修复：链接失效是用户环境变化（拔盘/移动文件）
                             的结果，应用替用户"修好"它反而可能指向错误的地方。 -->
                        <span v-if="it.dangling" class="dangling-tag" :title="danglingKeepTitle(it.linkSrc)">
                          <Icon name="alert" :size="11" /> 链接已失效
                        </span>
                      </td>
                      <td class="num">{{ humanBytes(it.size) }}</td>
                      <td><span :class="stateCls(it.state)">{{ STATE_LABEL[it.state] ?? it.state }}</span></td>
                      <td class="err-cell" :title="it.err">{{ it.err }}</td>
                      <td class="ops-col">
                        <!-- 软链接合并的找回路径：直达保留源（M290/M297）。
                             判据用 negation 而不是新写一个"是不是软链接"：
                             同一条规则只许有一处定义（offersInAppUndo），这里只是它的另一面。
                             ★ 这里**不挂** :disabled="store.busy"（T2 复看时撤掉的）：它只是打开
                               一个文件夹，不写后端状态，也不在 `opsRunning` 互斥门那八个绑定里，
                               与 GroupCard 的「打开所在文件夹」同一形状（那个从不因忙而灰）。
                               何况点"回撤"被拒之后，用户往往正是想先去看看那份数据在哪儿——
                               把唯一的找回出口灰掉是帮倒忙。 -->
                        <button v-if="!offersInAppUndo(m.kind) && it.linkSrc" class="btn-ghost xs"
                          :title="revealKeepTitle(it.linkSrc)"
                          @click="revealKeep(it)">直达保留原目录</button>
                        <button v-if="canUndoItem(m, it)" class="btn-ghost xs"
                          :disabled="store.busy"
                          :title="store.busyTip || '仅回撤此文件（恢复回原位置）'"
                          @click="undoOne(m, it)">
                          {{ undoingItem === it.id ? '执行中…' : (it.state === 'undo_failed' ? '重试回撤' : '回撤') }}
                        </button>
                      </td>
                    </tr>
                  </tbody>
                </table>
              </td>
            </tr>
          </template>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style scoped>
.records-view { flex: 1; display: flex; flex-direction: column; overflow: hidden; }
.tabs {
  margin: var(--sp-3) var(--page-gutter) 0; padding: 6px 10px;
  display: flex; align-items: center; gap: var(--sp-2);
}
.tabs button { background: none; color: var(--text-2); padding: 6px var(--sp-3); border-radius: var(--r-md); font: inherit; }
.tabs button.on { background: var(--primary-weak); color: var(--primary-ink); font-weight: 600; }
.spacer { flex: 1; }
.confirm-tip { font-size: var(--fs-sm); color: var(--danger-ink); }
.body { flex: 1; overflow-y: auto; padding: var(--sp-3) var(--page-gutter) var(--sp-5); }
.hist-table { width: 100%; border-collapse: collapse; font-size: var(--fs-sm); }
.hist-table th, .hist-table td { padding: 9px var(--sp-3); text-align: left; border-bottom: 1px solid var(--border); }
.hist-table th { color: var(--text-3); font-weight: 500; white-space: nowrap; }
.hist-table tr:last-child td { border-bottom: none; }
.hist-table tr.current td { background: var(--primary-weak); }
.mono { font-variant-numeric: tabular-nums; white-space: nowrap; }
.num { font-variant-numeric: tabular-nums; white-space: nowrap; }
.roots { max-width: 320px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--mono); }
.cleaned { color: var(--text-3); margin-left: 6px; }
.ops-col { white-space: nowrap; }
.ops-col button + button { margin-left: var(--sp-2); }
.ops-col .del:hover { color: var(--danger-ink); border-color: var(--danger-ink); }
/* ---------- 清理记录 ---------- */
.undo-hint { font-size: var(--fs-sm); color: var(--text-3); }
.hist-table tr.expanded td { background: var(--primary-weak); }
.kind-badge {
  display: inline-block; padding: 1px var(--sp-2); border-radius: 999px;
  font-size: var(--fs-xs, 12px); border: 1px solid var(--border); color: var(--text-2);
}
.kind-badge.k-delete { color: var(--danger-ink); border-color: var(--danger-ink); }
.kind-badge.k-trash { color: var(--primary-ink); border-color: var(--primary-ink); }
.no-undo { margin-left: 6px; font-size: var(--fs-xs, 12px); color: var(--text-3); }
/* 「数据在保留源，需手动放回」——这一格摆的是**可行动作**，不是坏消息，
   所以用中性色而不是 .no-undo 的灰、更不是 .dangling-tag 的红（红留给真出错的那一行）。
   为什么做成可见文本而不是只挂 title：04 §6.50 M293 定了 UIA 能读 title、但 OS 那层
   浮窗合成不出来 ⇒ "文案挂在元素上"证得了，"用户看得见"证不了（M286 同一条取向）。 */
.keep-hint {
  margin-left: 6px; padding: 0 6px; border-radius: 999px;
  font-size: var(--fs-xs, 12px); color: var(--text-2);
  border: 1px solid var(--border);
}
.detail-row > td { background: var(--bg-2, rgba(127, 127, 127, 0.05)); padding: var(--sp-1) var(--sp-3) var(--sp-3); }
.detail-loading { padding: var(--sp-3); color: var(--text-3); font-size: var(--fs-sm); }
.item-table { width: 100%; border-collapse: collapse; font-size: var(--fs-xs, 12px); }
.item-table th, .item-table td { padding: 6px 10px; text-align: left; border-bottom: 1px solid var(--border); }
.item-table th { color: var(--text-3); font-weight: 500; white-space: nowrap; }
.item-table tr:last-child td { border-bottom: none; }
.st { display: inline-block; padding: 1px 7px; border-radius: 999px; white-space: nowrap; }
.st-ok { color: var(--primary-ink); background: var(--primary-weak); }
.st-done { color: var(--text-2); background: rgba(127, 127, 127, 0.14); }
/* 状态胶囊：底色弱红一律走 --danger-weak。原先三处各自写成 rgba(220,80,80,·)
   的 α 变体——那是**亮色谱**的红：暗色主题里 --danger 已换成 #f87171 一系，
   写死的 RGB 不跟随，浅红底配深色 bg-panel 会变成脏红。 */
.st-bad { color: var(--danger-ink); background: var(--danger-weak); }
.st-mute { color: var(--text-3); background: rgba(127, 127, 127, 0.1); }
.err-cell { max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--danger-ink); }
/* 悬空链接行：整行淡红底 + 行内红标。
   为什么要整行着色而不是只标一个图标：明细表可能有几十行，
   悬空项是需要用户**动手处理**的（回撤或重新放回保留文件），
   只标图标在长表里会被扫过去。底色把视线拉住，图标说明原因。 */
.row-dangling > td { background: var(--danger-weak); }
.dangling-tag {
  display: inline-flex; align-items: center; gap: 3px;
  margin-left: 6px; padding: 0 6px; border-radius: 999px;
  font-size: var(--fs-xs); white-space: nowrap;
  color: var(--danger-ink); background: var(--danger-weak);
}
/* 空态样式与 ResultView 空态同节奏 */
.empty { text-align: center; color: var(--text-2); padding: 56px var(--sp-5); }
.empty-ico { color: var(--text-3); display: block; margin: 0 auto 10px; }
.empty-title { font-size: var(--fs-lg); font-weight: 600; color: var(--text); }
.empty-desc { margin-top: var(--sp-2); font-size: var(--fs-sm); color: var(--text-3); }
.empty-actions { margin-top: var(--sp-4); display: flex; gap: var(--sp-3); justify-content: center; }
</style>
