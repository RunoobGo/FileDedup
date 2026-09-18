<script setup lang="ts">
// 结果页（M3 版）：统计条 + 保留策略工具栏 + 操作按钮 + 执行反馈 + 组列表。
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useScanStore } from '../stores/scan'
import { useToastStore } from '../stores/toast'
import GroupCard from '../components/GroupCard.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import Icon from '../components/Icon.vue'
import { humanBytes, formatCount } from '../utils/format'

const store = useScanStore()
const toast = useToastStore()
const confirmKind = ref<'trash' | 'delete' | 'move' | 'hardlink' | null>(null)
const keepKind = ref('shortest')
const keepDirInput = ref('')

// store.confirmOpen 单一出口：弹框期间挂起全局快捷键（App.vue 用它拦截
// Space/Cmd+A/Delete，避免确认框打开时选中集被悄悄扩大），关闭后自动恢复。
// 修复点：原先由各按钮/回调手工赋值，trash/hardlink 分支漏置、onConfirm 漏复位，
// 导致确认过一次后拦截条件永久为假。
watch(confirmKind, v => { store.confirmOpen = !!v })

// C12：切页时本组件卸载，但局部 confirmKind 不会自动复位——切回结果页会
// 自动重开确认弹窗（且与 store.confirmOpen 脱钩，快捷键拦截失真）。卸载时复位。
// 此处仍手工清 confirmOpen：组件作用域已停，上面的 watch 不会再触发。
onUnmounted(() => {
  confirmKind.value = null
  store.confirmOpen = false
})

onMounted(() => {
  if (store.hasResult && store.groups.length === 0) store.loadResultPage(false)
})

// 列表替换时的勾选清理由 store 统一处理（Y8：默认不勾选，需显式选择）
function changeSort() { store.loadResultPage(false) }
function changeExt() { store.loadResultPage(false) }

function onScroll(e: Event) {
  const el = e.target as HTMLElement
  if (el.scrollTop + el.clientHeight >= el.scrollHeight - 200) {
    // Y7：滚动追加受放量上限约束；loadingPage 防止快速滚动重复排队拉取
    if (
      store.groups.length < store.totalGroups &&
      store.groups.length < store.loadCap &&
      !store.loadingPage
    ) {
      store.loadResultPage(true)
    }
  }
}

// 已达上限时按钮标注「继续加载」并放开一档
const capped = computed(() => store.groups.length >= store.loadCap && store.totalGroups > store.groups.length)
const moreLabel = computed(() =>
  capped.value
    ? `继续加载（已加载 ${formatCount(store.groups.length)} 组，点击再取一档）`
    : `加载更多（${formatCount(store.groups.length)}/${formatCount(store.totalGroups)}）`,
)

async function applyKeep() {
  await store.applyKeep(keepKind.value, store.keepDirs)
}

const keepDirsDisabled = computed(() => keepKind.value === 'directory' && store.keepDirs.length === 0)

async function pickKeepDir() {
  const { api } = await import('../wails')
  try {
    const dir = await api.selectDirectory()
    if (dir) store.addKeepDir(dir)
  } catch (e: any) {
    toast.notifyError('选择目录失败', e)
  }
}

function addKeepDirInput() {
  store.addKeepDir(keepDirInput.value)
  keepDirInput.value = ''
}

// 只改 confirmKind：store.confirmOpen 的复位由上方 watch 统一负责（不再手工赋值）
function onConfirm(targetDir?: string) {
  const kind = confirmKind.value!
  confirmKind.value = null
  store.executeOp(kind, targetDir, kind === 'delete')
}
</script>

<template>
  <div class="result-view">
    <!-- v0.5.0 功能 3：历史结果提示条 -->
    <div v-if="store.histResult" class="panel hist-banner">
      当前为历史结果（保存于 {{ new Date(store.histResult.savedAt * 1000).toLocaleString('zh-CN', { hour12: false }) }}），清理前会逐文件校验内容。
    </div>
    <!-- 统计条（P1-2：无重复组且无失败项时整块隐藏，不再渲染全 0 统计与无效的排序/过滤控件） -->
    <div v-if="store.totalGroups > 0 || store.failed.length" class="statbar panel">
      <template v-if="store.totalGroups > 0">
        <div class="stat"><b>{{ formatCount(store.totalGroups) }}</b> 重复组</div>
        <div class="stat"><b>{{ humanBytes(store.reclaimableTotal) }}</b> 可释放</div>
      </template>
      <!-- P2-3：失败项入口原是可点击 <div>，鼠标可达但键盘完全到不了（无 tabindex、
           无键盘处理、无 role）。改用原生 <button>，role/聚焦/Enter/Space 全部由元素自带。 -->
      <button v-if="store.failed.length" type="button" class="stat warn"
        @click="store.failedOpen = true" title="查看失败清单">
        <b>{{ formatCount(store.failed.length) }}</b> 失败项
      </button>
      <div class="spacer"></div>
      <template v-if="store.totalGroups > 0">
        <select v-model="store.resultSort" @change="changeSort" aria-label="结果排序方式">
          <option value="reclaimable">按可释放空间</option>
          <option value="size">按文件大小</option>
          <option value="count">按文件数</option>
        </select>
        <input v-model="store.resultExt" @change="changeExt" type="text" aria-label="按扩展名过滤"
          placeholder="扩展名，如 .jpg" style="width: 120px" />
        <button v-if="store.totalGroups > store.groups.length" class="btn-ghost"
          :class="{ capped }" @click="store.loadMore()">
          {{ moreLabel }}
        </button>
      </template>
    </div>

    <!-- 保留策略 + 操作工具栏（M3） -->
    <!-- P0-4：拆成两条稳定的整行——第 1 行「保留策略 + 选择状态」，第 2 行「操作按钮」。
         原先依赖 flex 换行，在 MinWidth(920) 下会塌成 3 行并把「永久删除」挤到单独一行。 -->
    <div v-if="store.hasResult && store.totalGroups > 0" class="toolbar panel">
      <div class="trow">
        <div class="keep">
          <!-- P2-3：`.lbl` 是 span，不构成 label 关联；补 aria-label 让下拉框有名称 -->
          <span class="lbl" aria-hidden="true">保留策略</span>
          <select v-model="keepKind" aria-label="保留策略">
            <option value="shortest">路径最短（默认）</option>
            <option value="newest">最新</option>
            <option value="oldest">最旧</option>
            <option value="directory">指定目录优先级…</option>
          </select>
          <button class="btn-ghost" :disabled="keepDirsDisabled || store.busy"
            :title="store.busyTip || '按所选策略标出保留项'" @click="applyKeep">应用</button>
          <button class="btn-ghost" :disabled="store.busy"
            :title="store.busyTip || '清除决策回到默认建议'" @click="store.clearKeep()">重置</button>
        </div>
        <span class="sel-info" aria-live="polite">
          <template v-if="store.selectedFiles.length">
            已选 <b>{{ store.selectedFiles.length }}</b> 项 / <b>{{ humanBytes(store.selectedBytes) }}</b>
          </template>
          <template v-else>未选择——点「全选」选中全部冗余项</template>
        </span>
      </div>
      <!-- 多目录优先级列表：序号即优先级，命中多个目录时保留最靠前目录内的文件 -->
      <div v-if="keepKind === 'directory'" class="keepdirs">
        <div v-for="(d, i) in store.keepDirs" :key="d" class="kd-row">
          <span class="kd-idx" :title="`优先级 ${i + 1}`">{{ i + 1 }}</span>
          <span class="kd-path" :title="d">{{ d }}</span>
          <button class="btn-ghost xs" :disabled="i === 0" title="上移（提高优先级）"
            aria-label="上移目录" @click="store.moveKeepDir(i, -1)">↑</button>
          <button class="btn-ghost xs" :disabled="i === store.keepDirs.length - 1"
            title="下移（降低优先级）" aria-label="下移目录" @click="store.moveKeepDir(i, 1)">↓</button>
          <button class="btn-ghost xs" title="移除该目录" aria-label="移除目录"
            @click="store.removeKeepDir(i)">✕</button>
        </div>
        <div class="kd-add">
          <button class="btn-ghost" @click="pickKeepDir">＋ 添加目录</button>
          <input v-model="keepDirInput" type="text" aria-label="粘贴目录路径"
            placeholder="粘贴目录路径后回车添加" @keyup.enter="addKeepDirInput" />
          <span v-if="!store.keepDirs.length" class="kd-hint">
            尚未添加目录：按优先级从高到低排列，组内文件命中多个目录时保留最靠前目录内的那份
          </span>
        </div>
      </div>
      <div class="ops">
        <button class="btn-ghost" :class="{ 'btn-emph': store.selectedFiles.length === 0 }"
          title="选中全部冗余项（保留项不可勾选）" @click="store.selectAll()">全选</button>
        <button class="btn-ghost" @click="store.clearSelection()">清除</button>
        <button class="btn-primary" :disabled="store.selectedFiles.length === 0 || store.busy"
          :title="store.busyTip || '把所选重复文件移入系统回收站'" @click="confirmKind = 'trash'">移入回收站</button>
        <button class="btn-ghost" :disabled="store.selectedFiles.length === 0 || store.busy"
          :title="store.busyTip || '移动到本会话授权的目录'" @click="confirmKind = 'move'">移动到…</button>
        <button class="btn-ghost" :disabled="store.selectedFiles.length === 0 || store.busy"
          :title="store.busyTip || '替换为指向保留文件的硬链接（同卷）'"
          @click="confirmKind = 'hardlink'">硬链接合并</button>
        <button class="btn-danger" :disabled="store.selectedFiles.length === 0 || store.busy"
          :title="store.busyTip || '不可恢复，需二次确认'" @click="confirmKind = 'delete'">永久删除</button>
      </div>
    </div>

    <!-- 操作执行反馈（M3-T06） -->
    <div v-if="store.opsRunning" class="opsbar panel">
      正在处理 <b>{{ store.opsProgress?.Done ?? 0 }}</b> / {{ store.opsProgress?.Total }} …
      <div class="bar"><div class="fill"
        :style="{ width: ((store.opsProgress?.Done ?? 0) / Math.max(1, store.opsProgress?.Total ?? 1)) * 100 + '%' }"></div></div>
      <!-- P2：大批量清理（网络盘、回收站卡住）必须能中止，否则界面永久锁在"执行中" -->
      <button class="btn-ghost" title="停止派发剩余条目；已完成的部分不会回滚"
        @click="store.cancelOp()">中止</button>
    </div>
    <div v-else-if="store.opsResult" class="opsbar panel done">
      <template v-if="store.opsResult.OK.length">
        <span class="ok"><Icon name="check" :size="13" /> 成功 {{ formatCount(store.opsResult.OK.length) }}（释放 {{ humanBytes(store.opsResult.Reclaimed) }}）</span>
        <button class="btn-ghost" @click="store.openTrash()">打开回收站</button>
      </template>
      <span v-if="store.opsResult.Skipped.length" class="skip"><Icon name="skip" :size="13" /> 已跳过 {{ formatCount(store.opsResult.Skipped.length) }}（文件已消失）</span>
      <!-- P2：中止后必须说清"还有多少没处理"，否则用户无法判断是否需要重跑 -->
      <span v-if="store.opsResult.Cancelled?.length" class="skip"><Icon name="skip" :size="13" /> 未处理 {{ formatCount(store.opsResult.Cancelled.length) }}（已中止，仍在列表中）</span>
      <button v-if="store.opsResult.Failed.length" type="button" class="fail"
        @click="store.failedOpen = true"><Icon name="alert" :size="13" /> 失败 {{ formatCount(store.opsResult.Failed.length) }}（查看）</button>
      <button class="x" title="关闭结果提示" aria-label="关闭结果提示"
        @click="store.opsResult = null"><Icon name="close" :size="13" /></button>
    </div>

    <!-- 组列表 -->
    <div class="list" @scroll="onScroll">
      <!-- P1-2：空态给出行动出口，而不是一句提示 + 大片留白 -->
      <div v-if="!store.hasResult" class="empty panel">
        <div class="empty-title">暂无结果</div>
        <p class="empty-desc">先在「扫描」页选择目录并执行一次扫描。</p>
        <div class="empty-actions">
          <button class="btn-primary" @click="store.switchView('scan')">去扫描</button>
        </div>
      </div>
      <div v-else-if="store.totalGroups === 0" class="empty panel">
        <Icon class="empty-ico" name="check-circle" :size="32" :stroke="1.7" />
        <div class="empty-title">没有发现重复文件</div>
        <p class="empty-desc">当前扫描范围内没有可合并的重复项，换个目录再试试。</p>
        <div class="empty-actions">
          <button class="btn-primary" @click="store.switchView('scan')">扫描其它目录</button>
          <button v-if="store.failed.length" class="btn-ghost" @click="store.failedOpen = true">
            查看失败项（{{ store.failed.length }}）
          </button>
        </div>
      </div>
      <GroupCard v-for="g in store.groups" :key="g.groupID" :group="g" />
    </div>

    <!-- close/confirm 都只改 confirmKind，store.confirmOpen 由 watch 统一复位 -->
    <ConfirmDialog v-if="confirmKind" :kind="confirmKind" @close="confirmKind = null" @confirm="onConfirm" />
  </div>
</template>

<style scoped>
.result-view { flex: 1; display: flex; flex-direction: column; overflow: hidden; }
.statbar, .toolbar { margin: var(--sp-3) var(--page-gutter) 0; padding: 10px 14px; display: flex; align-items: center; gap: var(--sp-3); flex-wrap: wrap; }
.hist-banner { flex: none; margin: var(--sp-3) var(--page-gutter) 0; padding: 8px 14px; font-size: var(--fs-sm); color: var(--text-2); }
.stat { font-size: var(--fs-sm); color: var(--text-2); white-space: nowrap; }
.stat b { font-size: var(--fs-lg); color: var(--text); margin-right: 4px; font-variant-numeric: tabular-nums; }
.stat.warn, .stat.warn b { color: var(--danger-ink); }
/* P2-3：失败项入口现在是 <button>，抹掉原生按钮的底与内边距，使其与其它 .stat 视觉一致 */
button.stat { background: none; padding: 0; font-family: inherit; }
button.stat:hover b { text-decoration: underline; }
.spacer { flex: 1; }
/* P0-4：纵向两行固定结构，不再依赖 flex 换行 —— 任何窗口宽度下都是 2 行，
   破坏性按钮永远与其它操作同处一行（原先 920px 下会孤立成第三行）。 */
.toolbar { margin-top: 8px; flex-direction: column; align-items: stretch; gap: var(--sp-3); }
.trow { display: flex; align-items: center; justify-content: space-between; gap: var(--sp-3); }
.keep, .ops { display: flex; align-items: center; gap: var(--sp-2); flex-wrap: nowrap; }
.keep { min-width: 0; }
.ops { justify-content: flex-end; }
.lbl { color: var(--text-2); font-size: var(--fs-sm); }
.sel-info { font-size: var(--fs-sm); color: var(--text-2); flex: none; }
.sel-info b { color: var(--primary-ink); }
.opsbar { margin: 8px 16px 0; padding: 10px 14px; display: flex; align-items: center; gap: var(--sp-4); font-size: var(--fs-sm); }
.opsbar .bar { flex: 1; height: 6px; background: var(--bg-hover); border-radius: var(--r-sm); overflow: hidden; }
.opsbar .fill { height: 100%; background: var(--primary); transition: width 0.2s; }
/* P2-1：结果条三种态各自带一个线性图标；用 inline-flex + gap 保证图标与文字间距确定，
   不再依赖模板里那个易被压缩掉的空白字符。 */
.done .ok, .done .skip, .done .fail { display: inline-flex; align-items: center; gap: 4px; }
.done .ok { color: var(--success-ink); }
.done .skip { color: var(--text-3); }
.done .fail { color: var(--danger-ink); background: none; padding: 0; font-family: inherit; }
.done .fail:hover { text-decoration: underline; }
.done .x { margin-left: auto; background: none; color: var(--text-3); padding: 2px 6px; display: inline-flex; align-items: center; }
.list { flex: 1; overflow-y: auto; padding: var(--sp-2) var(--page-gutter) var(--sp-5); }
/* P1-2：空态 = 标题 + 说明 + 行动按钮 */
.empty { text-align: center; color: var(--text-2); padding: 56px 24px; }
/* P2-1：原 「🎉」 换成统一的线性图标（check-circle，成功色），随主题变色、无 emoji 固有彩色 */
.empty-ico { color: var(--success-ink); display: block; margin: 0 auto 10px; }
.empty-title { font-size: var(--fs-lg); font-weight: 600; color: var(--text); }
.empty-desc { margin-top: 8px; font-size: var(--fs-sm); color: var(--text-3); }
.empty-actions { margin-top: var(--sp-4); display: flex; gap: var(--sp-3); justify-content: center; }
/* Y7：达到放量上限时以警示色提示需显式继续加载（边框属图形，需 ≥3:1） */
.btn-ghost.capped { color: var(--warn-ink); border-color: var(--warn-ink); }
/* Y8：未勾选时弱强调「全选」，引导用户显式选择（不再默认全选） */
.btn-ghost.btn-emph { border-color: var(--primary); color: var(--primary-ink); }
/* 多目录优先级列表 */
.keepdirs { margin-top: 2px; display: flex; flex-direction: column; gap: 4px; }
.kd-row { display: flex; align-items: center; gap: var(--sp-2); font-size: var(--fs-sm); }
.kd-idx { flex: none; width: 18px; height: 18px; border-radius: 50%; background: var(--primary);
  color: #fff; font-size: 11px; display: inline-flex; align-items: center; justify-content: center; }
.kd-path { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text); }
.btn-ghost.xs { padding: 1px 6px; font-size: 12px; line-height: 1.5; }
.kd-add { display: flex; align-items: center; gap: var(--sp-2); }
.kd-add input { flex: 1; min-width: 160px; }
.kd-hint { color: var(--text-3); font-size: var(--fs-sm); }
</style>
