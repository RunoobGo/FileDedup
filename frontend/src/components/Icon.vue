<script setup lang="ts">
// P2-1：全站统一线性图标（内联 SVG，24 网格，stroke=currentColor）。
//
// 取代原先并存的三套“图标语言”：
//   A 文字/几何符号  ◎扫描 ⧉结果 ⚙设置 ▾/▸折叠
//   B 单色 dingbat   ★保留 ✕冗余 ✓成功 ↷跳过
//   C 带固有颜色的 emoji  👁预览 📂打开文件夹 🎉空态
// 三者的字形粗细、基线、光学尺寸各不相同，且 emoji 自带彩色、随平台字体变化，
// 与单色 UI 冲突（同一排按钮里 👁 是彩色，旁边的 ✕ 是文字色）。
//
// 统一后的收益（全部可测）：
//   1. 颜色由 `currentColor` 继承 —— 图标随文字一起响应 hover / active / 主题切换，
//      不再有“emoji 换不了色”的死角；
//   2. 尺寸由 width/height 精确决定 —— 不再受字体度量的行盒影响，图标不再参与基线抖动；
//   3. 线宽、端点、圆角全站一致，光学重量统一。
//
// 几何数据见 ./icons.ts。纯装饰，恒 aria-hidden；需要可访问名称的按钮请把名称
// 放在按钮自身的 aria-label 上（见 P2-3）。
import { ICONS, ICON_SIZE } from './icons'
import type { IconName } from './icons'

const props = withDefaults(defineProps<{ name: IconName; size?: number; stroke?: number }>(), {
  size: 16,
  stroke: 2,
})
const def = ICONS[props.name]
</script>

<template>
  <svg
    class="ico"
    :width="size"
    :height="size"
    :viewBox="`0 0 ${ICON_SIZE} ${ICON_SIZE}`"
    fill="none"
    stroke="currentColor"
    :stroke-width="stroke"
    stroke-linecap="round"
    stroke-linejoin="round"
    :data-icon="name"
    aria-hidden="true"
    focusable="false"
  >
    <path v-for="(p, i) in def.d" :key="i" :d="p" />
    <circle v-if="def.circle" :cx="def.circle[0]" :cy="def.circle[1]" :r="def.circle[2]" />
  </svg>
</template>

<style scoped>
/* 关键：图标是行内元素，需与文字基线对齐；flex 容器里它会自动成为 flex item，
   此处的 vertical-align 不起作用也不冲突。 */
.ico {
  display: inline-block;
  vertical-align: -0.14em;
  flex: none;
}
</style>
