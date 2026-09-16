<script setup lang="ts">
import { useId } from 'vue'

/**
 * FileDedup 品牌标记：两页叠放的文档（右上折角）+ 正面一页上的内容指纹。
 * 母题取自参考图，「指纹 = 内容哈希」正是本项目最直白的隐喻。
 *
 * 改这个文件前先读三条约束：
 *
 * 1. 全部走 currentColor，不写死颜色。侧边栏、暗色主题、填充按钮上都能直接复用。
 *
 * 2. 后页被前页遮住的部分用 <mask> 抹掉，而不是「把前页填成背景色」——
 *    后者换主题/换背景就会穿帮。<mask> 必须挂在**不带 transform** 的外层 <g> 上：
 *    掩膜内容是在「引用它的那个元素自身的坐标系」里解释的，被遮元素一旦带
 *    transform，挡片会被一起平移旋转，结果就是把后页整页抹掉。
 *
 * 3. 折线能否分辨由「环距 vs 笔画」决定（48 网格下环距 2.6、笔画 1.65）。
 *    实测 40px 起环间开始分得开、48px 完全清楚，故默认 44px。
 *    **不要把 size 调到 32 以下** —— 指纹会糊成一团；那种尺寸应改用无指纹的简化版。
 *
 * 几何由一个一次性生成器产出（Python，见 docs/ui-logo-2026-09-16.md 的复现步骤），
 * 参数是照着 1:1 实渲染对照图定的，不要凭手感调数值。
 */
withDefaults(defineProps<{ size?: number }>(), { size: 44 })

// useId 产出的是唯一但含非法字符的串，清洗后用作 mask id，避免多实例互相串掩膜
const maskId = `fd-logo-${useId().replace(/[^a-zA-Z0-9_-]/g, '')}`

/** 一页文档的轮廓（右上角留出折角缺口），21×26.5 */
const SHEET = 'M1.6 0H15.8L21.0 5.2V24.9A1.6 1.6 0 0 1 19.4 26.5H1.6A1.6 1.6 0 0 1 0 24.9V1.6A1.6 1.6 0 0 1 1.6 0Z'
/** 折角翻起的小三角 */
const FOLD = 'M15.8 0L21.0 5.2L15.8 5.2Z'
/** 指纹：3 环 + 核心小圈，缺口由内向外递减（48° → 16°），开口朝下 */
const RINGS = [
  'M8.87 16.98A2.60 2.96 0 1 1 12.73 16.98',
  'M8.04 20.03A5.20 5.93 0 1 1 12.58 20.57',
  'M7.14 22.85A7.80 8.89 0 1 1 11.34 23.87',
  'M10.07 16.23A1.30 1.48 0 1 1 11.53 16.23',
]
</script>

<template>
  <svg
    class="logo-mark"
    :width="size"
    :height="size"
    viewBox="0 0 48 48"
    fill="none"
    stroke="currentColor"
    stroke-linecap="round"
    stroke-linejoin="round"
    role="img"
    aria-label="FileDedup"
  >
    <defs>
      <!-- 遮挡掩膜：整块放行，把正面那页连同外扩描边涂黑以挖出间隙 -->
      <mask :id="maskId" maskUnits="userSpaceOnUse" x="-80" y="-80" width="240" height="240">
        <rect x="-80" y="-80" width="240" height="240" fill="#fff" />
        <g transform="translate(1.4,1.2) rotate(4,10.5,13.25)">
          <path :d="SHEET" fill="#000" stroke="#000" stroke-width="5" />
        </g>
      </mask>
    </defs>

    <g transform="translate(14.895,11.02) scale(1.2176)">
      <g :mask="`url(#${maskId})`">
        <g transform="translate(-6.4,-5.6) rotate(-9,10.5,13.25)">
          <path :d="SHEET" stroke-width="1.8" />
          <path :d="FOLD" stroke-width="1.5" />
        </g>
      </g>

      <g transform="translate(1.4,1.2) rotate(4,10.5,13.25)">
        <path :d="SHEET" stroke-width="1.8" />
        <path :d="FOLD" stroke-width="1.5" />
        <path v-for="d in RINGS" :key="d" :d="d" stroke-width="1.65" />
      </g>
    </g>
  </svg>
</template>

<style scoped>
.logo-mark {
  display: block;
  flex: none;
}
</style>
