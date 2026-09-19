<script setup lang="ts">
// 失败清单抽屉（M2-T08）：扫描/操作失败项查看与导出。
// failed 数据由 store 统一维护（scan:done / ops:done 均在 store 处理），此处仅展示。
import { ref } from 'vue'
import { useScanStore } from '../stores/scan'
import { useToastStore } from '../stores/toast'
import { formatCount } from '../utils/format'
import { useModal } from '../composables/useModal'

const store = useScanStore()
const toast = useToastStore()

// P2-3：浮层语义 + 焦点管理（打开聚焦、Tab 循环、关闭归还焦点）。
// 本组件由 App.vue 常驻挂载、靠 store.failedOpen 控制显隐，因此必须把「是否打开」传进去。
const dlgRef = ref<HTMLElement | null>(null)
useModal(dlgRef, () => store.failedOpen)

function copyAll() {
  const text = store.failed.map(f => `${f.Stage}\t${f.Path}\t${f.Err}`).join('\n')
  // C10：剪贴板写入可能被拒绝（无授权/非安全上下文），rejection 须接住
  navigator.clipboard?.writeText(text).catch((e: any) => {
    toast.notifyError('复制失败', e)
  })
}
</script>

<template>
  <transition name="fade">
    <div v-if="store.failedOpen" class="mask" @click.self="store.failedOpen = false">
      <div
        ref="dlgRef"
        class="drawer panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby="failed-drawer-title"
        tabindex="-1"
      >
        <div class="d-head">
          <span id="failed-drawer-title">失败清单（{{ formatCount(store.failed.length) }}）</span>
          <div>
            <button class="btn-ghost" @click="copyAll">复制全部</button>
            <button class="btn-ghost" @click="store.failedOpen = false">关闭</button>
          </div>
        </div>
        <div class="d-body">
          <div v-if="store.failed.length === 0" class="empty">无失败项</div>
          <template v-else>
            <!-- P1-4：三列栅格 + 列头，路径列起点对齐 -->
            <div class="d-cols" aria-hidden="true">
              <span>阶段</span><span>路径</span><span>错误原因</span>
            </div>
            <div v-for="(f, i) in store.failed" :key="i" class="item">
              <span class="stage">{{ f.Stage }}</span>
              <span class="path" :title="f.Path">{{ f.Path }}</span>
              <span class="err" :title="f.Err">{{ f.Err }}</span>
            </div>
          </template>
        </div>
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
  width: 640px; max-width: 90vw; height: 100%;
  display: flex; flex-direction: column; border-radius: 0;
}
.d-head {
  display: flex; justify-content: space-between; align-items: center;
  padding: var(--sp-4); font-weight: 600; border-bottom: 1px solid var(--border);
}
.d-head > div { display: flex; gap: var(--sp-2); }
.d-body {
  flex: 1; overflow-y: auto; padding: 8px;
  /* P1-4：阶段 / 路径 / 错误 三列共用同一套轨道，保证三行起点一致 */
  --fail-cols: 56px minmax(0, 1.2fr) minmax(0, 1fr);
}
.empty { color: var(--text-3); text-align: center; padding: 40px 0; }
.d-cols {
  display: grid; grid-template-columns: var(--fail-cols); gap: var(--sp-2);
  padding: 0 8px 6px; font-size: var(--fs-sm); color: var(--text-3);
  border-bottom: 1px solid var(--border); margin-bottom: 4px;
}
.item {
  display: grid; grid-template-columns: var(--fail-cols); gap: var(--sp-2);
  align-items: start; padding: 6px 8px; border-radius: var(--r-md); font-size: var(--fs-sm);
}
.item:hover { background: var(--bg-hover); }
.stage { color: var(--warn-ink); white-space: nowrap; }
.path { font-family: var(--mono); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; user-select: text; }
/* 原先被 max-width:180px + nowrap 截成 49~167px，最关键的错误原因全被切掉。
   改为占满整列并允许换行 —— 抽屉的唯一用途就是排障，错误信息必须完整可读。 */
.err { color: var(--danger-ink); overflow-wrap: anywhere; user-select: text; }
</style>
