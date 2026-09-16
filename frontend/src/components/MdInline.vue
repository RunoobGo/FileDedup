<script setup lang="ts">
// P2-7：行内 Markdown 片段的渲染。
// 只走 Vue 模板插值与属性绑定（无 v-html），因此磁盘上的任意内容都不可能注入 HTML/脚本；
// 链接的 href 已在 utils/markdown.ts 里做过协议白名单过滤。
import { computed } from 'vue'
import { parseInline } from '../utils/markdown'
import type { Seg } from '../utils/markdown'

const props = defineProps<{ text: string }>()
// 必须是 computed 而非一次性求值：MarkdownView 用下标 key 渲染列表，
// 换一个文件预览时组件实例会被复用（只更新 props），一次性求值会让行内片段停留在上一份文本。
const segs = computed<Seg[]>(() => parseInline(props.text))
</script>

<template>
  <template v-for="(s, i) in segs" :key="i">
    <code v-if="s.k === 'code'" class="md-code">{{ s.text }}</code>
    <strong v-else-if="s.k === 'b'">{{ s.text }}</strong>
    <em v-else-if="s.k === 'i'">{{ s.text }}</em>
    <del v-else-if="s.k === 'strike'">{{ s.text }}</del>
    <a v-else-if="s.k === 'link'" :href="s.href" target="_blank" rel="noopener noreferrer">{{ s.text }}</a>
    <template v-else>{{ s.text }}</template>
  </template>
</template>

<style scoped>
.md-code {
  font-family: var(--mono);
  font-size: 0.94em;
  padding: 1px 5px;
  border-radius: var(--r-sm);
  background: var(--bg-hover);
  color: var(--text);
}
a { color: var(--primary-ink); }
</style>
