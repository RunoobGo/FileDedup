<script setup lang="ts">
// 预览面板（M2 基础版：文本/HEX/图片；M4 缩略图增强）。
//
// P2-7：头部原先只有一行路径 + 关闭，用户无法从面板判断"这是什么文件、多大、
// 何时修改"，也无法回到磁盘定位；Markdown 源码原样铺开（`# 世界观设定 v12`）
// 等于让预览面板退化成 cat。现在：
//   1. 标题行显示文件名，右侧集中动作（查看源码/渲染预览、在文件夹中打开、关闭）；
//   2. 第二行是元信息行：完整路径 + 「类型 · 大小 · 修改时间」三枚 chip；
//   3. Markdown 默认**渲染**呈现，可一键切回源码核对原文。
import { ref, computed, watch } from 'vue'
import { useScanStore } from '../stores/scan'
import { useModal } from '../composables/useModal'
import { humanBytes, formatMtime } from '../utils/format'
import { isMarkdownExt } from '../utils/markdown'
import MarkdownView from './MarkdownView.vue'

const store = useScanStore()

// P2-3：浮层语义 + 焦点管理（打开聚焦到「关闭」，Tab 循环，关闭归还焦点）
const dlgRef = ref<HTMLElement | null>(null)
useModal(dlgRef, () => !!store.preview)

// 扩展名从文件名取（FileView.name），避免为一次展示新增后端字段
const ext = computed(() => {
  const n = store.preview?.name ?? ''
  const i = n.lastIndexOf('.')
  return i > 0 ? n.slice(i).toLowerCase() : ''
})

const isMd = computed(() => store.preview?.kind === 'text' && isMarkdownExt(ext.value))

// Markdown 默认渲染（审计问题的本体就是"源码原样展示"），可切回源码逐字核对。
// 每次打开新文件都重置阅读模式——预览是"快速判断"场景，不该继承上一个文件的模式。
//
// 性能护栏：本渲染器是同步的，面板也不做虚拟滚动，代价随文档线性增长。
// 实测（Chromium / Apple Silicon，rAF 采样）：
//    20 KB →  2 091 节点 ·  36 ms
//   100 KB → 10 440 节点 · 141 ms
//   400 KB → 41 724 节点 · 450 ms   ← 肉眼可见的卡顿，且 4 万节点会长期驻留内存
// 因此超过阈值时**默认退回源码**（源码只是一个 <pre>，代价恒定），
// 但保留手动渲染入口 —— 把选择权交给用户，而不是替他把界面冻住半秒。
//
// ★ M117（04 §6.11 FE-12）：按当前的后端上限，这道护栏**打不到**——预览内容只有
// PreviewFile 一个来源，文本腿固定截在前 4 KiB（app.go 的 textLimit = 4 << 10），
// 而 isMd 又要求 kind === 'text' ⇒ content.length > 64 KiB 恒假。上面那三档是
// **渲染器本体**的代价读数（真造 400 KB 文档即得），不是这条分支的可达读数。
// 保留它的理由是"后端上限以后变大时不必重新想起来"，不是"它现在在挡什么"。
// 也不调低阈值：那会让正常 md 文件默认退回源码，属改行为，不属本轮。
const MD_RENDER_MAX = 64 * 1024
const overRenderCap = computed(() => (store.preview?.content.length ?? 0) > MD_RENDER_MAX)

const rendered = ref(true)
watch(() => store.preview, (p) => {
  rendered.value = !(p && p.content.length > MD_RENDER_MAX)
})

const typeLabel = computed(() => {
  const p = store.preview
  if (!p) return ''
  const kindName =
    p.kind === 'image' ? '图片' : p.kind === 'text' ? '文本' : p.kind === 'hex' ? '二进制' : '其他'
  if (!ext.value) return kindName
  const e = ext.value.slice(1).toUpperCase()
  return isMarkdownExt(ext.value) ? `${e} · Markdown` : `${e} · ${kindName}`
})

function close() {
  store.preview = null
}
</script>

<template>
  <transition name="fade">
    <div v-if="store.preview" class="mask" @click.self="close">
      <div
        ref="dlgRef"
        class="panel box"
        role="dialog"
        aria-modal="true"
        aria-label="文件预览"
        tabindex="-1"
      >
        <div class="p-head">
          <span class="fname" :title="store.preview.path">
            {{ store.preview.name || store.preview.path }}
          </span>
          <div class="acts">
            <button
              v-if="isMd"
              class="btn-ghost sm"
              :aria-pressed="rendered"
              :title="overRenderCap && !rendered
                ? `正文 ${humanBytes(store.preview.content.length)}，超出 ${humanBytes(MD_RENDER_MAX)} 渲染上限，已默认显示源码；仍可手动渲染`
                : ''"
              @click="rendered = !rendered"
            >
              {{ rendered ? '查看源码' : '渲染预览' }}
            </button>
            <button
              v-if="store.preview.path"
              class="btn-ghost sm"
              aria-label="在文件夹中打开该文件"
              @click="store.revealPreview()"
            >
              在文件夹中打开
            </button>
            <button class="btn-ghost" @click="close">关闭 (Esc)</button>
          </div>
        </div>

        <div class="p-meta">
          <span class="path" :title="store.preview.path">{{ store.preview.path }}</span>
          <span class="chips">
            <span class="chip">{{ typeLabel }}</span>
            <span class="chip">{{ humanBytes(store.preview.size) }}</span>
            <span class="chip">{{ formatMtime(store.preview.mtime) }}</span>
            <span v-if="overRenderCap" class="chip chip-note">默认源码</span>
          </span>
        </div>

        <div class="p-body">
          <img v-if="store.preview.kind === 'image'"
            :src="`data:${store.preview.mime};base64,${store.preview.content}`" alt="预览" />
          <MarkdownView v-else-if="isMd && rendered" :text="store.preview.content" />
          <pre v-else-if="store.preview.kind === 'text'">{{ store.preview.content }}</pre>
          <pre v-else-if="store.preview.kind === 'hex'" class="hex">{{ store.preview.content }}</pre>
          <div v-else class="empty">{{ store.preview.content || '二进制文件' }}</div>
        </div>
      </div>
    </div>
  </transition>
</template>

<style scoped>
.mask {
  position: fixed; inset: 0; background: rgba(0, 0, 0, 0.45);
  display: flex; align-items: center; justify-content: center; z-index: 110;
}
.box { width: 720px; max-width: 90vw; max-height: 80vh; display: flex; flex-direction: column; border-radius: var(--r-lg); }

.p-head {
  display: flex; align-items: center; gap: var(--sp-3);
  padding: 10px 14px;
}
.fname {
  flex: 1; min-width: 0;
  font-size: var(--fs-lg); font-weight: 600; color: var(--text);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.acts { display: flex; align-items: center; gap: var(--sp-2); flex: none; }
.acts .btn-ghost { padding: 4px 10px; font-size: var(--fs-sm); }

/* P2-7：元信息行。与标题行用同一条分隔线体系，但底色下沉一档，
   让「标题 = 主体信息 / 元信息 = 辅助信息」在视觉层次上直接读得出来。 */
.p-meta {
  display: flex; align-items: center; gap: var(--sp-3); flex-wrap: wrap;
  padding: 7px 14px;
  border-top: 1px solid var(--border);
  border-bottom: 1px solid var(--border);
  background: var(--bg-hover);
}
.path {
  flex: 1; min-width: 0;
  font-family: var(--mono); font-size: var(--fs-xs); color: var(--text-2);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.chips { display: flex; align-items: center; gap: var(--sp-2); flex: none; }
.chip {
  padding: 1px 7px; border: 1px solid var(--border); border-radius: var(--r-sm);
  background: var(--bg-panel); color: var(--text-2);
  font-size: var(--fs-xs); white-space: nowrap;
}
/* 超限提示。文字用 --text 而非 --warn-ink：--warn-ink(#b45309) 对 --warn-weak
   叠加 --bg-hover 后的实际底色只有约 4.1:1（亮）/ 4.2:1（暗），够不到正文 AA；
   --text 在该底色上两套主题都远超 4.5:1。保留琥珀底色作为「注意」的信号。 */
.chip-note { background: var(--warn-weak); border-color: transparent; color: var(--text); }

.p-body { overflow: auto; padding: 12px; }
.p-body img { max-width: 100%; max-height: 60vh; display: block; margin: 0 auto; }
pre { font-family: var(--mono); font-size: var(--fs-sm); line-height: 1.55; white-space: pre-wrap; user-select: text; }
pre.hex { white-space: pre; font-size: var(--fs-sm); }
.empty { text-align: center; color: var(--text-3); padding: 30px 0; }
</style>
