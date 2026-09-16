<script setup lang="ts">
// P2-7：Markdown 块级渲染。
//
// 安全前提：解析结果只经 Vue 模板渲染（插值 + 属性绑定），全程不碰 v-html，
// 因此磁盘文件里的 `<script>` / `<img onerror=...>` 只会作为**文本**显示。
// 唯一需要把外部字符串写进属性的地方是链接 href，已由 utils/markdown.ts 的
// safeHref() 做协议白名单过滤。行内片段交给 MdInline 处理。
//
// 渲染范围刻意保持"轻量"：标题 / 段落 / 有序无序列表 / 引用 / 围栏代码 / 分隔线。
// 不支持的语法（表格、原生 HTML、嵌套强调）按纯文本原样呈现——宁可少渲染，
// 也不要半吊子的富文本带来的歧义与风险。
import { computed } from 'vue'
import { parseMarkdown } from '../utils/markdown'
import MdInline from './MdInline.vue'

const props = defineProps<{ text: string }>()
const blocks = computed(() => parseMarkdown(props.text))

// 动态标题标签：:is 需要具体标签名字符串
function hTag(level: number): string {
  return 'h' + Math.min(6, Math.max(1, level))
}
</script>

<template>
  <div class="md">
    <template v-for="(b, i) in blocks" :key="i">
      <component :is="hTag(b.level)" v-if="b.t === 'h'"><MdInline :text="b.text" /></component>

      <p v-else-if="b.t === 'p'"><MdInline :text="b.text" /></p>

      <ul v-else-if="b.t === 'ul'">
        <li v-for="(it, j) in b.items" :key="j"><MdInline :text="it" /></li>
      </ul>

      <ol v-else-if="b.t === 'ol'">
        <li v-for="(it, j) in b.items" :key="j"><MdInline :text="it" /></li>
      </ol>

      <blockquote v-else-if="b.t === 'quote'"><MdInline :text="b.text" /></blockquote>

      <pre v-else-if="b.t === 'code'" class="md-pre"><code>{{ b.text }}</code></pre>

      <hr v-else-if="b.t === 'hr'" />
    </template>
  </div>
</template>

<style scoped>
.md {
  font-size: var(--fs-base);
  line-height: 1.65;
  color: var(--text);
  user-select: text;
  word-break: break-word;
}
/* 首尾不留外边距，避免面板内出现"悬空"的空白带 */
.md > :first-child { margin-top: 0; }
.md > :last-child { margin-bottom: 0; }

.md :is(h1, h2, h3, h4, h5, h6) {
  line-height: 1.3;
  font-weight: 600;
  margin: var(--sp-4) 0 var(--sp-2);
}
.md h1 { font-size: var(--fs-xl); }
.md h2 { font-size: var(--fs-lg); }
.md h3, .md h4, .md h5, .md h6 { font-size: var(--fs-base); }
.md h4, .md h5, .md h6 { color: var(--text-2); }

.md p { margin: 0 0 var(--sp-3); }

.md :is(ul, ol) { margin: 0 0 var(--sp-3); padding-left: 22px; }
.md li { margin: 2px 0; }
.md ul li { list-style: disc; }
.md ol li { list-style: decimal; }

.md blockquote {
  margin: 0 0 var(--sp-3);
  padding: var(--sp-2) var(--sp-3);
  border-left: 3px solid var(--border);
  background: var(--bg-hover);
  border-radius: 0 var(--r-sm) var(--r-sm) 0;
  color: var(--text-2);
}

.md-pre {
  margin: 0 0 var(--sp-3);
  padding: var(--sp-3);
  background: var(--bg-hover);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  overflow: auto;
}
.md-pre code {
  font-family: var(--mono);
  font-size: var(--fs-sm);
  line-height: 1.55;
  white-space: pre;
  user-select: text;
}

.md hr { margin: var(--sp-4) 0; border: none; border-top: 1px solid var(--border); }
</style>
