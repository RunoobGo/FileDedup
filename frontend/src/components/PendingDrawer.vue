<script setup lang="ts">
// 拟处理清单抽屉（2026-09-23，设计段 specs/2026-09-23-pending-files-query-design.md §4）。
//
// 回答的问题是"现在点执行，到底会动哪几个文件"——结果页那一行计数（已选 N / 生效 M）
// 只给数字，这一层给逐行明细与被排除项的归因。数据由 store 统一拉取（快照式），
// 本组件只展示；后端 PendingCount 是分界索引：前段"将处理"、后段"不会处理"。
import { ref, computed } from 'vue'
import { useScanStore } from '../stores/scan'
import { humanBytes, formatCount } from '../utils/format'
import { firstExcludedIndex } from '../utils/pending'
import { useModal } from '../composables/useModal'

const store = useScanStore()

// P2-3 同族：浮层语义 + 焦点管理；常驻挂载由 App.vue 靠 store.pendingOpen 控显隐。
const dlgRef = ref<HTMLElement | null>(null)
useModal(dlgRef, () => store.pendingOpen)

const data = computed(() => store.pendingData)
// 分界：当前页里第一段结束的位置（pending 段整体排在前面，跨页时可能页内没有交集）。
// C3 / R-前端-4：边界数学收进 utils/pending 的纯函数（.vue 打不进 node --test，
// 内联时"整页都被排除"那格零覆盖且曾漏挂标题）。
const firstExcluded = computed(() => firstExcludedIndex(data.value))
const excludedOnPage = computed(() =>
  firstExcluded.value >= 0 ? data.value!.rows.length - firstExcluded.value : 0,
)
</script>

<template>
  <transition name="fade">
    <div v-if="store.pendingOpen" class="mask" @click.self="store.closePendingDrawer()">
      <div
        ref="dlgRef"
        class="drawer panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby="pending-drawer-title"
        tabindex="-1"
      >
        <div class="d-head">
          <span id="pending-drawer-title">
            拟处理清单
            <template v-if="data">（将处理 <b>{{ formatCount(data.pendingCount) }}</b> 项
              <template v-if="data.total - data.pendingCount > 0">
                / 不会处理 {{ formatCount(data.total - data.pendingCount) }} 项
              </template>）</template>
          </span>
          <div>
            <select :value="store.pendingSort" aria-label="清单排序"
              @change="store.pendingSetSort(($event.target as HTMLSelectElement).value)">
              <option value="group">按分组序</option>
              <option value="size">按大小</option>
              <option value="path">按路径</option>
            </select>
            <button class="btn-ghost" @click="store.closePendingDrawer()">关闭</button>
          </div>
        </div>
        <div v-if="store.pendingError" class="d-err">清单拉取失败：{{ store.pendingError }}</div>
        <div class="d-body">
          <div v-if="!data && store.pendingLoading" class="empty">加载中…</div>
          <div v-else-if="!data" class="empty">无数据</div>
          <div v-else-if="data.rows.length === 0" class="empty">
            {{ data.total === 0 ? '没有勾选任何文件' : '本页之外没有条目' }}
          </div>
          <template v-else>
            <div class="d-cols" aria-hidden="true">
              <span>路径</span><span>大小</span><span>归属</span>
            </div>
            <div v-if="firstExcluded === 0" class="sec-head">以下均不会被处理</div>
            <template v-for="(r, i) in data.rows" :key="r.id">
              <div v-if="i === firstExcluded && i !== 0" class="sec-head">
                以下 {{ excludedOnPage }} 项不会被处理（原因见「归属」列）
              </div>
              <div class="item" :class="{ excl: !r.pending }">
                <span class="path" :title="r.path || `（已不在结果集，ID ${r.id}）`">
                  {{ r.path || `—（ID ${r.id}）` }}
                </span>
                <span class="size">{{ r.path ? humanBytes(r.size) : '—' }}</span>
                <span class="why">
                  <template v-if="r.pending">组 #{{ r.groupId }}</template>
                  <template v-else-if="r.reason === 'keep'">保留项 · 硬拒绝</template>
                  <template v-else-if="r.reason === 'outside'">不在优先文件夹内</template>
                  <template v-else-if="r.reason === 'excluded'">「不处理」目录内</template>
                  <template v-else>已不在结果集</template>
                </span>
              </div>
            </template>
          </template>
        </div>
        <div v-if="data && data.total > data.pageSize" class="d-foot">
          <button class="btn-ghost xs" :disabled="data.page === 0"
            aria-label="上一页" @click="store.pendingGotoPage(data.page - 1)">← 上一页</button>
          <span>第 {{ data.page + 1 }} / {{ store.pendingTotalPages }} 页</span>
          <button class="btn-ghost xs" :disabled="data.page + 1 >= store.pendingTotalPages"
            aria-label="下一页" @click="store.pendingGotoPage(data.page + 1)">下一页 →</button>
        </div>
        <div class="d-note">清单是打开这一刻的快照；改勾选后重新打开即重取。</div>
      </div>
    </div>
  </transition>
</template>

<style scoped>
.mask {
  position: fixed; inset: 0; background: rgba(0, 0, 0, 0.35);
  display: flex; justify-content: flex-end; z-index: 100;
}
.drawer {
  width: 720px; max-width: 92vw; height: 100%;
  display: flex; flex-direction: column; border-radius: 0;
}
.d-head {
  display: flex; justify-content: space-between; align-items: center;
  padding: var(--sp-4); font-weight: 600; border-bottom: 1px solid var(--border);
}
.d-head > div { display: flex; gap: var(--sp-2); align-items: center; }
.d-err { padding: var(--sp-2) var(--sp-4); color: var(--danger-ink); font-size: var(--fs-sm); }
.d-body {
  flex: 1; overflow-y: auto; padding: var(--sp-2);
  --pend-cols: minmax(0, 1.6fr) 72px minmax(0, 0.9fr);
}
.empty { color: var(--text-3); text-align: center; padding: 40px 0; }
.d-cols {
  display: grid; grid-template-columns: var(--pend-cols); gap: var(--sp-2);
  padding: 0 var(--sp-2) 6px; font-size: var(--fs-sm); color: var(--text-3);
  border-bottom: 1px solid var(--border); margin-bottom: var(--sp-1);
}
.sec-head {
  padding: 10px var(--sp-2) var(--sp-1); font-size: var(--fs-sm); color: var(--text-3);
  border-top: 1px dashed var(--border); margin-top: 6px;
}
.item {
  display: grid; grid-template-columns: var(--pend-cols); gap: var(--sp-2);
  align-items: start; padding: 6px var(--sp-2); border-radius: var(--r-md); font-size: var(--fs-sm);
}
.item:hover { background: var(--bg-hover); }
.item.excl { color: var(--text-3); }
.path { font-family: var(--mono); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; user-select: text; }
.size { text-align: right; font-variant-numeric: tabular-nums; }
.why { overflow-wrap: anywhere; }
.d-foot {
  display: flex; justify-content: space-between; align-items: center;
  padding: var(--sp-2) var(--sp-4); border-top: 1px solid var(--border); font-size: var(--fs-sm);
}
.d-note { padding: var(--sp-1) var(--sp-4) 10px; font-size: var(--fs-sm); color: var(--text-3); }
</style>
