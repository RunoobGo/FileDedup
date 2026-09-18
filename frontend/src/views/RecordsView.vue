<script setup lang="ts">
// 记录页（v0.5.0 功能 3）：扫描历史列表 + 恢复/重扫/删除；清理记录标签 Task 12 填充。
import { computed, onMounted, ref } from 'vue'
import { useScanStore } from '../stores/scan'
import { humanBytes, formatCount } from '../utils/format'
import Icon from '../components/Icon.vue'
import type { HistoryMeta } from '../wails'

const store = useScanStore()
const tab = ref<'scans' | 'ops'>('scans')
// ConfirmDialog 是清理操作专用（勾选数/移动目标耦合），历史「清空」用两步式行内确认
const confirmClear = ref(false)

onMounted(() => { store.refreshHistory() })

const loading = computed(() => store.histLoading)

function fmtTime(unixSec: number): string {
  return new Date(unixSec * 1000).toLocaleString('zh-CN', { hour12: false })
}

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
</script>

<template>
  <div class="records-view">
    <div class="tabs panel">
      <button :class="{ on: tab === 'scans' }" @click="tab = 'scans'">扫描历史</button>
      <button :class="{ on: tab === 'ops' }" @click="tab = 'ops'">清理记录</button>
      <span class="spacer"></span>
      <template v-if="tab === 'scans' && store.histList.length">
        <button v-if="!confirmClear" class="btn-ghost" @click="confirmClear = true">清空</button>
        <template v-else>
          <span class="confirm-tip">确认清空全部历史？（不影响当前结果集）</span>
          <button class="btn-danger" @click="clearAll">确认清空</button>
          <button class="btn-ghost" @click="confirmClear = false">取消</button>
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
            <td class="mono">{{ fmtTime(m.savedAt) }}</td>
            <td class="roots" :title="m.roots.join('\n')">{{ rootsSummary(m) }}</td>
            <td class="num">{{ formatCount(m.groups) }}</td>
            <td class="num">
              {{ formatCount(m.files) }}
              <span v-if="cleanedCount(m)" class="cleaned">已清理 {{ formatCount(cleanedCount(m)) }}</span>
            </td>
            <td class="num">{{ humanBytes(m.reclaimable) }}</td>
            <td class="ops-col">
              <button class="btn-primary" :disabled="loading || store.scanning || store.opsRunning"
                :title="store.scanning ? '扫描进行中' : store.opsRunning ? '清理操作进行中' : '恢复该结果并可继续清理'"
                @click="open(m)">恢复</button>
              <button class="btn-ghost" :disabled="store.scanning || store.opsRunning"
                title="按此配置重新扫描" @click="rescan(m)">重扫</button>
              <button class="btn-ghost del" title="删除该条历史" @click="del(m)">删除</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 标签二：清理记录（Task 12 接入回撤后填充数据源） -->
    <div v-else class="body">
      <div class="empty panel">
        <Icon class="empty-ico" name="undo" :size="32" :stroke="1.7" />
        <div class="empty-title">暂无清理记录</div>
        <p class="empty-desc">清理操作会自动留痕，并支持对回收站/移动/硬链接合并进行回撤。</p>
      </div>
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
/* 空态样式与 ResultView 空态同节奏 */
.empty { text-align: center; color: var(--text-2); padding: 56px 24px; }
.empty-ico { color: var(--text-3); display: block; margin: 0 auto 10px; }
.empty-title { font-size: var(--fs-lg); font-weight: 600; color: var(--text); }
.empty-desc { margin-top: 8px; font-size: var(--fs-sm); color: var(--text-3); }
.empty-actions { margin-top: var(--sp-4); display: flex; gap: var(--sp-3); justify-content: center; }
</style>
