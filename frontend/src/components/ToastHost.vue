<script setup lang="ts">
// 提示宿主（Y6）：固定在主区域右下角，可堆叠、自动消失、可手动关闭。
// 主题色随应用 data-theme 变量切换。
import { useToastStore } from '../stores/toast'

const toast = useToastStore()

const ICONS: Record<string, string> = { error: '✕', warn: '!', info: 'i', success: '✓' }
</script>

<template>
  <div class="toast-host" aria-live="polite" aria-atomic="false">
    <TransitionGroup name="toast">
      <div v-for="t in toast.toasts" :key="t.id" class="toast" :class="t.kind" role="alert">
        <span class="tag">{{ ICONS[t.kind] ?? '!' }}</span>
        <span class="msg">{{ t.msg }}</span>
        <button class="x" title="关闭提示" aria-label="关闭提示" @click="toast.dismiss(t.id)">✕</button>
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
  gap: 8px;
  max-width: min(420px, calc(100vw - 36px));
  pointer-events: none; /* 宿主不拦截点击，交互由单个提示承担 */
}
.toast {
  pointer-events: auto;
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 9px 12px;
  border-radius: var(--radius);
  background: var(--bg-panel);
  border: 1px solid var(--border);
  border-left: 3px solid var(--text-3);
  box-shadow: 0 6px 18px rgba(0, 0, 0, 0.12);
  font-size: 12.5px;
  color: var(--text);
}
.toast.error { border-left-color: var(--danger); }
.toast.warn { border-left-color: var(--warn); }
.toast.success { border-left-color: var(--success); }
.toast.info { border-left-color: var(--primary); }
.tag {
  flex: none;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 10px;
  line-height: 1;
  margin-top: 1px;
  background: var(--bg-hover);
  color: var(--text-2);
}
.toast.error .tag { background: var(--danger-weak); color: var(--danger); }
.toast.success .tag { background: rgba(16, 185, 129, 0.12); color: var(--success); }
.msg { flex: 1; word-break: break-word; user-select: text; }
.x {
  flex: none;
  background: none;
  color: var(--text-3);
  padding: 0 2px;
  font-size: 11px;
}
.x:hover { color: var(--danger); }

/* 进出场：轻微上滑渐入，避免遮挡视线 */
.toast-enter-active,
.toast-leave-active { transition: opacity 0.18s ease, transform 0.18s ease; }
.toast-enter-from,
.toast-leave-to { opacity: 0; transform: translateY(6px); }
</style>
