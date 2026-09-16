<script setup lang="ts">
// 结果页（M3 版）：统计条 + 保留策略工具栏 + 操作按钮 + 执行反馈 + 组列表。
import { computed, onMounted, ref } from 'vue'
import { useScanStore } from '../stores/scan'
import { useToastStore } from '../stores/toast'
import GroupCard from '../components/GroupCard.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import { humanBytes } from '../utils/format'

const store = useScanStore()
const toast = useToastStore()
const confirmKind = ref<'trash' | 'delete' | 'move' | 'hardlink' | null>(null)
const keepKind = ref('shortest')
const keepDir = ref('')

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
    ? `继续加载（已加载 ${store.groups.length} 组，点击再取一档）`
    : `加载更多（${store.groups.length}/${store.totalGroups}）`,
)

async function applyKeep() {
  await store.applyKeep(keepKind.value, keepDir.value || undefined)
}

async function pickKeepDir() {
  const { api } = await import('../wails')
  try {
    keepDir.value = await api.selectDirectory()
  } catch (e: any) {
    toast.notifyError('选择目录失败', e)
  }
}

function onConfirm(targetDir?: string) {
  const kind = confirmKind.value!
  confirmKind.value = null
  store.executeOp(kind, targetDir, kind === 'delete')
}
</script>

<template>
  <div class="result-view">
    <!-- 统计条 -->
    <div class="statbar panel">
      <div class="stat"><b>{{ store.totalGroups }}</b> 重复组</div>
      <div class="stat"><b>{{ humanBytes(store.reclaimableTotal) }}</b> 可释放</div>
      <div class="stat warn" v-if="store.failed.length" style="cursor: pointer"
        @click="store.failedOpen = true" title="查看失败清单">
        <b>{{ store.failed.length }}</b> 失败项
      </div>
      <div class="spacer"></div>
      <select :value="store.resultSort" @change="changeSort">
        <option value="reclaimable">按可释放空间</option>
        <option value="size">按文件大小</option>
        <option value="count">按文件数</option>
      </select>
      <input :value="store.resultExt" @change="changeExt" type="text"
        placeholder="扩展名，如 .jpg" style="width: 120px" />
      <button v-if="store.totalGroups > store.groups.length" class="btn-ghost"
        :class="{ capped }" @click="store.loadMore()">
        {{ moreLabel }}
      </button>
    </div>

    <!-- 保留策略 + 操作工具栏（M3） -->
    <div v-if="store.hasResult" class="toolbar panel">
      <div class="keep">
        <span class="lbl">保留策略</span>
        <select v-model="keepKind">
          <option value="shortest">路径最短（默认）</option>
          <option value="newest">最新</option>
          <option value="oldest">最旧</option>
          <option value="directory">指定目录…</option>
        </select>
        <input v-if="keepKind === 'directory'" v-model="keepDir" type="text"
          placeholder="保留此目录下的文件" style="width: 200px" />
        <button v-if="keepKind === 'directory'" class="btn-ghost" @click="pickKeepDir">…</button>
        <button class="btn-ghost" @click="applyKeep">应用</button>
        <button class="btn-ghost" title="清除决策回到默认建议" @click="store.clearKeep()">重置</button>
      </div>
      <div class="ops">
        <span class="sel-info">
          <template v-if="store.selectedFiles.length">
            已选 <b>{{ store.selectedFiles.length }}</b> 项 / <b>{{ humanBytes(store.selectedBytes) }}</b>
          </template>
          <template v-else>未选择——勾选文件，或点「全选」选中全部冗余项（保留项不可选）</template>
        </span>
        <button class="btn-ghost" :class="{ 'btn-emph': store.selectedFiles.length === 0 }"
          @click="store.selectAll()">全选</button>
        <button class="btn-ghost" @click="store.clearSelection()">清除</button>
        <button class="btn-primary" :disabled="store.selectedFiles.length === 0"
          @click="confirmKind = 'trash'">移入回收站</button>
        <button class="btn-ghost" :disabled="store.selectedFiles.length === 0"
          @click="confirmKind = 'move'">移动到…</button>
        <button class="btn-ghost" :disabled="store.selectedFiles.length === 0"
          title="替换为指向保留文件的硬链接（同卷）"
          @click="confirmKind = 'hardlink'">硬链接合并</button>
        <button class="btn-danger" :disabled="store.selectedFiles.length === 0"
          @click="confirmKind = 'delete'">永久删除</button>
      </div>
    </div>

    <!-- 操作执行反馈（M3-T06） -->
    <div v-if="store.opsRunning" class="opsbar panel">
      正在处理 <b>{{ store.opsProgress?.Done ?? 0 }}</b> / {{ store.opsProgress?.Total }} …
      <div class="bar"><div class="fill"
        :style="{ width: ((store.opsProgress?.Done ?? 0) / Math.max(1, store.opsProgress?.Total ?? 1)) * 100 + '%' }"></div></div>
    </div>
    <div v-else-if="store.opsResult" class="opsbar panel done">
      <template v-if="store.opsResult.OK.length">
        <span class="ok">✓ 成功 {{ store.opsResult.OK.length }}（释放 {{ humanBytes(store.opsResult.Reclaimed) }}）</span>
        <button class="btn-ghost" @click="store.openTrash()">打开回收站</button>
      </template>
      <span v-if="store.opsResult.Skipped.length" class="skip">↷ 已跳过 {{ store.opsResult.Skipped.length }}（文件已消失）</span>
      <span v-if="store.opsResult.Failed.length" class="fail" style="cursor: pointer"
        @click="store.failedOpen = true">✕ 失败 {{ store.opsResult.Failed.length }}（查看）</span>
      <button class="x" @click="store.opsResult = null">✕</button>
    </div>

    <!-- 组列表 -->
    <div class="list" @scroll="onScroll">
      <div v-if="!store.hasResult" class="empty panel">暂无结果——请先在「扫描」页执行一次扫描</div>
      <div v-else-if="store.groups.length === 0" class="empty panel">没有发现重复文件 🎉</div>
      <GroupCard v-for="g in store.groups" :key="g.groupID" :group="g" />
    </div>

    <ConfirmDialog v-if="confirmKind" :kind="confirmKind" @close="confirmKind = null" @confirm="onConfirm" />
  </div>
</template>

<style scoped>
.result-view { flex: 1; display: flex; flex-direction: column; overflow: hidden; }
.statbar, .toolbar { margin: 12px 16px 0; padding: 10px 14px; display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.stat { font-size: 12px; color: var(--text-2); white-space: nowrap; }
.stat b { font-size: 14px; color: var(--text); margin-right: 4px; font-variant-numeric: tabular-nums; }
.stat.warn, .stat.warn b { color: var(--danger); }
.spacer { flex: 1; }
.toolbar { margin-top: 8px; justify-content: space-between; }
.keep, .ops { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.lbl { color: var(--text-2); font-size: 12px; }
.sel-info { font-size: 12px; color: var(--text-2); }
.sel-info b { color: var(--primary); }
.opsbar { margin: 8px 16px 0; padding: 10px 14px; display: flex; align-items: center; gap: 14px; font-size: 12px; }
.opsbar .bar { flex: 1; height: 6px; background: var(--bg-hover); border-radius: 3px; overflow: hidden; }
.opsbar .fill { height: 100%; background: var(--primary); transition: width 0.2s; }
.done .ok { color: var(--success); }
.done .skip { color: var(--text-3); }
.done .fail { color: var(--danger); }
.done .x { margin-left: auto; background: none; color: var(--text-3); padding: 2px 6px; }
.list { flex: 1; overflow-y: auto; padding: 10px 16px 20px; }
.empty { text-align: center; color: var(--text-3); padding: 60px 0; }
/* Y7：达到放量上限时以警示色提示需显式继续加载 */
.btn-ghost.capped { color: var(--warn); border-color: var(--warn); }
/* Y8：未勾选时弱强调「全选」，引导用户显式选择（不再默认全选） */
.btn-ghost.btn-emph { border-color: var(--primary); color: var(--primary); }
</style>
