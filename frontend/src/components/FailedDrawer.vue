<script setup lang="ts">
// 失败清单抽屉（M2-T08）：扫描/操作失败项查看与导出。
// failed 数据由 store 统一维护（scan:done / ops:done 均在 store 处理），此处仅展示。
import { useScanStore } from '../stores/scan'

const store = useScanStore()

function copyAll() {
  const text = store.failed.map(f => `${f.Stage}\t${f.Path}\t${f.Err}`).join('\n')
  navigator.clipboard?.writeText(text)
}
</script>

<template>
  <transition name="fade">
    <div v-if="store.failedOpen" class="mask" @click.self="store.failedOpen = false">
      <div class="drawer panel">
        <div class="d-head">
          <span>失败清单（{{ store.failed.length }}）</span>
          <div>
            <button class="btn-ghost" @click="copyAll">复制全部</button>
            <button class="btn-ghost" @click="store.failedOpen = false">关闭</button>
          </div>
        </div>
        <div class="d-body">
          <div v-if="store.failed.length === 0" class="empty">无失败项</div>
          <div v-for="(f, i) in store.failed" :key="i" class="item">
            <span class="stage">{{ f.Stage }}</span>
            <span class="path" :title="f.Path">{{ f.Path }}</span>
            <span class="err" :title="f.Err">{{ f.Err }}</span>
          </div>
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
  width: 560px; max-width: 90vw; height: 100%;
  display: flex; flex-direction: column; border-radius: 0;
}
.d-head {
  display: flex; justify-content: space-between; align-items: center;
  padding: 14px 16px; font-weight: 600; border-bottom: 1px solid var(--border);
}
.d-head > div { display: flex; gap: 8px; }
.d-body { flex: 1; overflow-y: auto; padding: 8px; }
.empty { color: var(--text-3); text-align: center; padding: 40px 0; }
.item {
  display: flex; gap: 8px; align-items: baseline;
  padding: 6px 8px; border-radius: 6px; font-size: 12px;
}
.item:hover { background: var(--bg-hover); }
.stage { color: var(--warn); white-space: nowrap; }
.path { font-family: var(--mono); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; user-select: text; }
.err { color: var(--danger); margin-left: auto; max-width: 180px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
