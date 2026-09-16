<script setup lang="ts">
// 重复组卡片：文件行 + 保留建议高亮（M2-T06）。
import { ref } from 'vue'
import type { GroupView } from '../wails'
import { api } from '../wails'
import { humanBytes, formatMtime } from '../utils/format'
import { useScanStore } from '../stores/scan'
import { useToastStore } from '../stores/toast'
import Icon from './Icon.vue'

const props = defineProps<{ group: GroupView }>()
const store = useScanStore()
const toast = useToastStore()
const expanded = ref(true)
// M3：勾选与操作状态由 store 管理

function reveal(id: number) {
  store.preview = null
  api.revealInFolder(id).catch((e: any) => toast.notifyError('打开文件夹失败', e))
}
</script>

<template>
  <div class="card panel" :style="{ '--n': group.files.length }">
    <div class="head" @click="expanded = !expanded">
      <Icon name="chevron-down" :size="14" :class="['chev', { closed: !expanded }]" />
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
        <!-- P1-5：用 label 包裹，把命中区域从原生控件的 13×13 扩到 26×26 -->
        <label
          class="check-wrap"
          :class="{ off: f.isKeep }"
          :title="f.isKeep ? '保留项（不可勾选）' : '勾选为待删除项'"
        >
          <input
            class="check"
            type="checkbox"
            :checked="store.selection.has(f.id)"
            :disabled="f.isKeep"
            :aria-label="f.isKeep ? `保留项 ${f.name}（不可勾选）` : `标记为删除 ${f.name}`"
            @change="store.toggleSelect(f.id)"
          />
        </label>
        <span
          class="keep-tag"
          :class="{ on: f.isKeep }"
          :title="f.isKeep ? '保留项（当前决策）' : '冗余项（勾选后将被删除）'"
        >
          <Icon :name="f.isKeep ? 'star' : 'close'" :size="12" />
          {{ f.isKeep ? '保留' : '冗余' }}
        </span>
        <span class="name" :title="f.path">{{ f.name }}</span>
        <span class="path" :title="f.path">{{ f.path }}</span>
        <span class="mtime">{{ formatMtime(f.mtime) }}</span>
        <button class="op" title="预览" aria-label="预览该文件" @click.stop="store.openPreview(f.id)">
          <Icon name="eye" :size="15" />
        </button>
        <button class="op" title="打开所在文件夹" aria-label="在文件夹中打开" @click.stop="reveal(f.id)">
          <Icon name="folder-open" :size="15" />
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.card { margin-bottom: var(--sp-3); contain-intrinsic-size: auto 140px; content-visibility: auto; }
.head {
  display: flex; align-items: center; gap: var(--sp-2);
  padding: 9px 14px; cursor: pointer; border-bottom: 1px solid var(--border);
}
/* P2-1：折叠指示由 ▾/▸ 两种字形改为一个线性图标 + 旋转；
   宽度由 12px 对齐到图标 14px，网格基线不变（head 为 flex 行，按 gap 排布）。 */
.chev { color: var(--text-3); width: 14px; height: 14px; transition: transform 0.18s ease; }
.chev.closed { transform: rotate(-90deg); }
.size { font-weight: 600; font-variant-numeric: tabular-nums; }
.sep, .count { color: var(--text-3); }
.reclaim { margin-left: auto; color: var(--primary-ink); font-weight: 600; }
.files { padding: 4px 8px 8px; }
.file {
  display: flex; align-items: center; gap: var(--sp-2);
  padding: 5px 8px; border-radius: var(--r-md);
}
.file:hover { background: var(--bg-hover); }
.file.keep { background: var(--success-weak); }
/* P1-5：原复选框命中区域仅 13×13，缩放下极易点空 —— 而点空会落到文件行上触发
   「设为当前项」，属于纯误操作。改为 label 承载命中区：视觉仍为 16×16（≥ WCAG 2.5.8
   的 16px 视觉下限），命中区 26×26。
   负纵向 margin 抵消撑高——26 − 8 = 18px，恰好等于原文件行的内容高度，
   因此行高维持 28px 不变（仅行内留白从 5px 视觉上收窄到 1px）。 */
.check-wrap {
  flex: 0 0 auto; display: flex; align-items: center; justify-content: center;
  width: 26px; height: 26px; margin: -4px 0; border-radius: var(--r-sm); cursor: pointer;
}
.check-wrap.off { cursor: default; }
.check-wrap:hover:not(.off) { background: var(--bg-hover); }
.check { width: 16px; height: 16px; cursor: inherit; }
/* accent-color 由 style.css 全局统一下发（P0-2），此处不再单独声明 */
.file.current { outline: 1.5px solid var(--primary); background: var(--primary-weak); }
/* P2-8②：每行都用「红色胶囊 + ✕ 冗余」标记，137 组下实测结果页出现 398 个红胶囊，
   整页被红色噪音淹没 —— 而"冗余"其实是**缺省状态**（每组只有 1 个保留项），
   把缺省态标成警示色本末倒置。
   现在：保留项 = 绿底胶囊（唯一的"决策"，需要跳出来）；冗余项 = 无底色 + --text-3 的弱标记。
   两者图标同尺寸、文字同为 2 个汉字、padding 相同，因此宽度一致（约 53px），
   标记列不会因标记类型不同而让文件名列左右跳动。
   弱标记的对比度：--text-3 对 --bg-panel 5.30:1、对 --bg-hover 4.73:1，均达 AA。 */
.keep-tag {
  display: inline-flex; align-items: center; gap: 3px;
  font-size: var(--fs-xs); padding: 1px 8px; border-radius: var(--r-sm); cursor: default;
  background: none; color: var(--text-3);
}
.keep-tag.on { background: var(--success-weak); color: var(--success-ink); }
.name { font-weight: 500; max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
/* P0-3：路径与修改时间是判断「该删哪个副本」的核心信息，
   由 11.5px/--text-3（2.42:1）提升到 12px/--text-2（亮色 7.5:1、暗色 6.8:1 on 面板底）。 */
.path {
  flex: 1; color: var(--text-2); font-family: var(--mono); font-size: var(--fs-sm);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap; user-select: text;
}
.mtime { color: var(--text-2); font-size: var(--fs-sm); white-space: nowrap; }
/* P1-5：原 26×20（👁）/ 26×25（📂），低于 28×28 的图标按钮下限。
   撑到 28×28 后用负纵向 margin 抵消（28 − 10 = 18px），行高维持 28px 不变。 */
.op {
  background: none; color: var(--text-3);
  display: flex; align-items: center; justify-content: center;
  min-width: 28px; min-height: 28px; margin: -5px 0;
  padding: 0; border-radius: var(--r-md); line-height: 1;
}
.op:hover { color: var(--primary); background: var(--bg-hover); }
</style>
