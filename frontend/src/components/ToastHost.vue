<script setup lang="ts">
// 提示宿主（Y6）：固定在主区域右下角，可堆叠、自动消失、可手动关闭。
// 主题色随应用 data-theme 变量切换。
import { useToastStore } from '../stores/toast'
import Icon from './Icon.vue'
// P2-1：角标由字符（✕ ! i ✓）改为统一线性图标。映射表放在 icons.ts 里统一定义，
// 保证同一语义在导航、按钮、角标各处都取到同一个图标。
import { KIND_ICONS } from './icons'

const toast = useToastStore()
</script>

<template>
  <!-- P2-3：宿主原先声明 aria-live="polite"，每条提示又带 role="alert"（隐含 assertive），
       同一区域出现两个互相矛盾的播报策略。改为只由单条提示承担 live 语义：
       错误用 role="alert"（assertive，需立即打断），其余用 role="status"（polite），
       宿主不再重复声明，消除嵌套 live region。 -->
  <div class="toast-host">
    <TransitionGroup name="toast">
      <div
        v-for="t in toast.toasts"
        :key="t.id"
        class="toast"
        :class="t.kind"
        :role="t.kind === 'error' ? 'alert' : 'status'"
      >
        <Icon class="toast-tag" :name="KIND_ICONS[t.kind] ?? 'alert'" :size="14" />
        <span class="msg">{{ t.msg }}</span>
        <button class="x" title="关闭提示" aria-label="关闭提示" @click="toast.dismiss(t.id)">
          <Icon name="close" :size="12" />
        </button>
      </div>
    </TransitionGroup>
  </div>
</template>

<style scoped>
.toast-host {
  position: fixed;
  right: 18px;
  bottom: 18px;
  z-index: 200; /* 高于预览面板(100)/失败抽屉/确认对话框(120) */
  display: flex;
  flex-direction: column;
  gap: var(--sp-2);
  max-width: min(420px, calc(100vw - 36px));
  pointer-events: none; /* 宿主不拦截点击，交互由单个提示承担 */
}
.toast {
  pointer-events: auto;
  display: flex;
  align-items: flex-start;
  gap: var(--sp-2);
  padding: 9px var(--sp-3);
  border-radius: var(--r-md);
  background: var(--bg-panel);
  border: 1px solid var(--border);
  border-left: 3px solid var(--text-3);
  box-shadow: 0 6px 18px rgba(0, 0, 0, 0.12);
  font-size: var(--fs-sm);
  color: var(--text);
}
.toast.error { border-left-color: var(--danger-ink); }
.toast.warn { border-left-color: var(--warn-ink); }
.toast.success { border-left-color: var(--success-ink); }
.toast.info { border-left-color: var(--primary-ink); }
/* P2-1：角标由字符改为 14px 线性图标；保留圆形弱底作为视觉锚点，
   去掉 font-size/line-height（图标尺寸已由 SVG 属性决定）。
   注意：类名必须避开全局样式表里的 `.tag`（那是 GroupCard 「图片组」徽章的样式，
   带 `padding: 1px 8px`）。此前角标也叫 `.tag`，全局 padding 会与这里的
   `width/height: 18px` + `box-sizing: border-box` 叠加，把 SVG 的**内容盒**压到
   2px 宽 —— 图标被 viewBox 等比缩放到 1px 的一个点（实测 getScreenCTM().a = 2/24）。
   改名后类名与全局徽章解耦，图标按声明的 14px 正常渲染。 */
.toast-tag {
  flex: none;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-top: 0;
  background: var(--bg-hover);
  color: var(--text-2);
}
.toast.error .toast-tag { background: var(--danger-weak); color: var(--danger-ink); }
.toast.warn .toast-tag { background: var(--warn-weak); color: var(--warn-ink); }
.toast.info .toast-tag { background: var(--primary-weak); color: var(--primary-ink); }
.toast.success .toast-tag { background: var(--success-weak); color: var(--success-ink); }
.msg { flex: 1; word-break: break-word; user-select: text; }
.x {
  flex: none;
  background: none;
  color: var(--text-3);
  display: flex;
  align-items: center;
  justify-content: center;
  min-width: 18px;
  min-height: 18px;
  padding: 0;
}
.x:hover { color: var(--danger-ink); }

/* 进出场：轻微上滑渐入，避免遮挡视线 */
.toast-enter-active,
.toast-leave-active { transition: opacity 0.18s ease, transform 0.18s ease; }
.toast-enter-from,
.toast-leave-to { opacity: 0; transform: translateY(6px); }
</style>
