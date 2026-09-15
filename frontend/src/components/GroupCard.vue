<script setup lang="ts">
// 重复组卡片：文件行 + 保留建议高亮（M2-T06）。
import { ref } from 'vue'
import type { GroupView } from '../wails'
import { api } from '../wails'
import { humanBytes, formatMtime } from '../utils/format'
import { useScanStore } from '../stores/scan'

const props = defineProps<{ group: GroupView }>()
const store = useScanStore()
const expanded = ref(true)
// M3：勾选与操作状态由 store 管理

function reveal(id: number) {
  store.preview = null
  api.revealInFolder(id).catch((e: any) => alert('打开文件夹失败: ' + String(e)))
}
</script>

<template>
  <div class="card panel" :style="{ '--n': group.files.length }">
    <div class="head" @click="expanded = !expanded">
      <span class="chev">{{ expanded ? '▾' : '▸' }}</span>
      <span class="size">{{ humanBytes(group.size) }}</span>
      <span class="sep">×</span>
      <span class="count">{{ group.files.length }} 个文件</span>
      <span class="reclaim">可释放 {{ humanBytes(group.reclaimable) }}</span>
      <span v-if="group.files.some(f => /\.(png|jpe?g|gif|webp|bmp|svg)$/i.test(f.path))" class="tag">图片组</span>
    </div>
    <div v-show="expanded" class="files">
      <div v-for="f in group.files" :key="f.id" class="file"
        :class="{ keep: f.isKeep, current: store.currentFileID === f.id }"
        @click="store.currentFileID = f.id" title="点击设为当前项（Space 预览）">
        <input
          class="check"
          type="checkbox"
          :checked="store.selection.has(f.id)"
          :disabled="f.isKeep"
          :title="f.isKeep ? '保留项（不可勾选）' : ''"
          @change="store.toggleSelect(f.id)"
        />
        <span class="keep-tag" :class="{ on: f.isKeep }" :title="f.isKeep ? '保留项（当前决策）' : ''">
          {{ f.isKeep ? '★ 保留' : '✕ 冗余' }}
        </span>
        <span class="name" :title="f.path">{{ f.name }}</span>
        <span class="path" :title="f.path">{{ f.path }}</span>
        <span class="mtime">{{ formatMtime(f.mtime) }}</span>
        <button class="op" title="预览" @click.stop="store.openPreview(f.id)">👁</button>
        <button class="op" title="打开所在文件夹" @click.stop="reveal(f.id)">📂</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.card { margin-bottom: 10px; contain-intrinsic-size: auto 140px; content-visibility: auto; }
.head {
  display: flex; align-items: center; gap: 8px;
  padding: 9px 14px; cursor: pointer; border-bottom: 1px solid var(--border);
}
.chev { color: var(--text-3); width: 12px; }
.size { font-weight: 600; font-variant-numeric: tabular-nums; }
.sep, .count { color: var(--text-3); }
.reclaim { margin-left: auto; color: var(--primary); font-weight: 600; }
.files { padding: 4px 8px 8px; }
.file {
  display: flex; align-items: center; gap: 8px;
  padding: 5px 8px; border-radius: 6px;
}
.file:hover { background: var(--bg-hover); }
.file.keep { background: rgba(16, 185, 129, 0.05); }
.check { accent-color: var(--primary); }
.file.current { outline: 1.5px solid var(--primary); background: var(--primary-weak); }
.keep-tag {
  font-size: 11px; padding: 1px 8px; border-radius: 4px; cursor: default;
  background: var(--danger-weak); color: var(--danger);
}
.keep-tag.on { background: rgba(16, 185, 129, 0.12); color: var(--success); }
.name { font-weight: 500; max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.path {
  flex: 1; color: var(--text-3); font-family: var(--mono); font-size: 11.5px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap; user-select: text;
}
.mtime { color: var(--text-3); font-size: 11.5px; white-space: nowrap; }
.op { background: none; padding: 2px 5px; color: var(--text-3); }
.op:hover { color: var(--primary); }
</style>
