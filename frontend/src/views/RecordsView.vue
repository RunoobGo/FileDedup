<script setup lang="ts">
// 记录页（v0.5.0 功能 3+4）：扫描历史列表 + 恢复/重扫/删除；清理记录 + 回撤。
import { computed, onMounted, ref, watch } from 'vue'
import { useScanStore } from '../stores/scan'
import { useToastStore } from '../stores/toast'
import { api } from '../wails'
import { humanBytes, formatCount, formatUnixSec } from '../utils/format'
import { undoBlockedTitle, undoTitleCodeForKind } from '../utils/undoReason'
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

// done 可撤；undo_failed 给修正后重试通道（与批量回撤的后端口径一致）
function canUndoItem(m: OpRecord, it: OpRecordItem): boolean {
  return m.undoable && (it.state === 'done' || it.state === 'undo_failed')
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
  confirmClearOps.value = false
  await store.clearOps()
}

// 明细表「去向」列：trash/move 看 destPath，hardlink 看 linkSrc
// destSummary 条目「去向」列的文案。
//
// 链接类条目（hardlink/symlink）的去向是"指向哪个保留源"，而非某个路径，
// 故这里统一渲染成「→ 目标路径」。软链接额外标注「软链接」字样：
// 后端的 isSymlink 标记来自 Lstat，能区分"当时建的链接"与"同栏但其实是
// 硬链接的记录"——不靠 OP_KIND_LABEL 猜，避免旧记录/异常记录被误标。
function destSummary(it: OpRecordItem): string {
  if (it.destPath) return it.destPath
  if (it.linkSrc) return it.isSymlink ? `软链接 → ${it.linkSrc}` : `链接到 ${it.linkSrc}`
  return '—'
}

// danglingTitle 悬空链接的悬浮说明。
//
// 悬空 = 链接本身还在，但它指向的保留文件已经不在原路径了
// （被删除、被移动、或所在磁盘未接入）。用户此时最需要知道的是
// 「我的文件没丢，数据在保留源那边；而且这一条仍然可以回撤」——
// 这正是它和"文件丢失"的根本区别，必须说清楚，否则用户会以为数据没了。
function danglingTitle(it: OpRecordItem): string {
  return `软链接已失效（悬空）：它指向的保留文件当前不可访问（可能已被删除、移动，或所在磁盘未接入）。\n` +
    `你的数据没有丢失——合并时磁盘上只保留了一份，而这一条只是指向它的路径替身。\n` +
    `目标：${it.linkSrc}\n` +
    `如需恢复成独立文件，点右侧「回撤」即可（备份仍在，回撤不依赖链接是否有效）。`
}
</script>

<template>
  <div class="records-view">
    <div class="tabs panel">
      <button :class="{ on: tab === 'scans' }" @click="tab = 'scans'">扫描历史</button>
      <button :class="{ on: tab === 'ops' }" @click="tab = 'ops'">清理记录</button>
      <span class="spacer"></span>
      <span v-if="tab === 'ops' && store.opsRunning" class="undo-hint">回撤执行中…</span>
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
          <!-- B3-1：账本在途时不可清空（后端同样拒绝，这里免掉"点了才报错"） -->
          <button v-if="!confirmClearOps" class="btn-ghost" :disabled="store.busy"
            :title="store.busyTip || '清空全部清理记录'" @click="confirmClearOps = true">清空</button>
          <template v-else>
            <span class="confirm-tip">确认清空清理记录？清空后未回撤的操作将无法再回撤</span>
            <button class="btn-danger" :disabled="store.busy" @click="clearAllOps">确认清空</button>
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
              <button class="btn-primary" :disabled="store.histBusy"
                :title="store.scanning ? '扫描进行中' : store.opsRunning ? '清理操作进行中' : loading ? '正在载入历史结果' : '恢复该结果并可继续清理'"
                @click="open(m)">恢复</button>
              <button class="btn-ghost" :disabled="store.histBusy"
                title="按此配置重新扫描" @click="rescan(m)">重扫</button>
              <button class="btn-ghost del" :disabled="store.histBusy"
                title="删除该条历史" @click="del(m)">删除</button>
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
              </td>
              <td class="num">
                {{ formatCount(m.items) }}
                <span v-if="m.failed" class="cleaned">失败 {{ m.failed }}</span>
              </td>
              <td class="num">{{ m.undone ? formatCount(m.undone) : '—' }}</td>
              <td class="num">{{ humanBytes(m.reclaimable) }}</td>
              <td class="ops-col">
                <button v-if="undoLeft(m) > 0" class="btn-primary"
                  :disabled="store.busy"
                  :title="store.opsRunning ? '操作执行中' : `恢复 ${undoLeft(m)} 项文件`"
                  @click="askUndo(m)">
                  {{ confirmUndoId === m.id ? `确认回撤 ${undoLeft(m)} 项` : '回撤' }}
                </button>
                <button v-else-if="m.undoable && m.done" class="btn-ghost" disabled>已回撤</button>
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
                        <span v-if="it.dangling" class="dangling-tag" :title="danglingTitle(it)">
                          <Icon name="alert" :size="11" /> 链接已失效
                        </span>
                      </td>
                      <td class="num">{{ humanBytes(it.size) }}</td>
                      <td><span :class="stateCls(it.state)">{{ STATE_LABEL[it.state] ?? it.state }}</span></td>
                      <td class="err-cell" :title="it.err">{{ it.err }}</td>
                      <td class="ops-col">
                        <button v-if="canUndoItem(m, it)" class="btn-ghost xs"
                          :disabled="store.busy"
                          :title="store.opsRunning ? '操作执行中' : '仅回撤此文件（恢复回原位置）'"
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
.tabs button { background: none; color: var(--text-2); padding: 6px 12px; border-radius: var(--r-md); font: inherit; }
.tabs button.on { background: var(--primary-weak); color: var(--primary-ink); font-weight: 600; }
.spacer { flex: 1; }
.confirm-tip { font-size: var(--fs-sm); color: var(--danger-ink); }
.body { flex: 1; overflow-y: auto; padding: var(--sp-3) var(--page-gutter) var(--sp-5); }
.hist-table { width: 100%; border-collapse: collapse; font-size: var(--fs-sm); }
.hist-table th, .hist-table td { padding: 9px 12px; text-align: left; border-bottom: 1px solid var(--border); }
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
  display: inline-block; padding: 1px 8px; border-radius: 999px;
  font-size: var(--fs-xs, 12px); border: 1px solid var(--border); color: var(--text-2);
}
.kind-badge.k-delete { color: var(--danger-ink); border-color: var(--danger-ink); }
.kind-badge.k-trash { color: var(--primary-ink); border-color: var(--primary-ink); }
.no-undo { margin-left: 6px; font-size: var(--fs-xs, 12px); color: var(--text-3); }
.detail-row > td { background: var(--bg-2, rgba(127, 127, 127, 0.05)); padding: 4px 12px 12px; }
.detail-loading { padding: 12px; color: var(--text-3); font-size: var(--fs-sm); }
.item-table { width: 100%; border-collapse: collapse; font-size: var(--fs-xs, 12px); }
.item-table th, .item-table td { padding: 6px 10px; text-align: left; border-bottom: 1px solid var(--border); }
.item-table th { color: var(--text-3); font-weight: 500; white-space: nowrap; }
.item-table tr:last-child td { border-bottom: none; }
.st { display: inline-block; padding: 1px 7px; border-radius: 999px; white-space: nowrap; }
.st-ok { color: var(--primary-ink); background: var(--primary-weak); }
.st-done { color: var(--text-2); background: rgba(127, 127, 127, 0.14); }
.st-bad { color: var(--danger-ink); background: rgba(220, 80, 80, 0.12); }
.st-mute { color: var(--text-3); background: rgba(127, 127, 127, 0.1); }
.err-cell { max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--danger-ink); }
/* 悬空链接行：整行淡红底 + 行内红标。
   为什么要整行着色而不是只标一个图标：明细表可能有几十行，
   悬空项是需要用户**动手处理**的（回撤或重新放回保留文件），
   只标图标在长表里会被扫过去。底色把视线拉住，图标说明原因。 */
.row-dangling > td { background: rgba(220, 80, 80, 0.08); }
.dangling-tag {
  display: inline-flex; align-items: center; gap: 3px;
  margin-left: 6px; padding: 0 6px; border-radius: 999px;
  font-size: 11px; white-space: nowrap;
  color: var(--danger-ink); background: rgba(220, 80, 80, 0.14);
}
.btn-ghost.xs { padding: 1px 6px; font-size: 12px; line-height: 1.5; }
/* 空态样式与 ResultView 空态同节奏 */
.empty { text-align: center; color: var(--text-2); padding: 56px 24px; }
.empty-ico { color: var(--text-3); display: block; margin: 0 auto 10px; }
.empty-title { font-size: var(--fs-lg); font-weight: 600; color: var(--text); }
.empty-desc { margin-top: 8px; font-size: var(--fs-sm); color: var(--text-3); }
.empty-actions { margin-top: var(--sp-4); display: flex; gap: var(--sp-3); justify-content: center; }
</style>
