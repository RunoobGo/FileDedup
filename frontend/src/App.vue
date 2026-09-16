<script setup lang="ts">
// 应用骨架：侧边导航 + 主工作区（01 §6.2 布局）+ 全局快捷键（M4-T05）。
import { onMounted, onUnmounted } from 'vue'
import { useScanStore } from './stores/scan'
import ScanView from './views/ScanView.vue'
import ResultView from './views/ResultView.vue'
import SettingsView from './views/SettingsView.vue'
import FailedDrawer from './components/FailedDrawer.vue'
import PreviewPanel from './components/PreviewPanel.vue'
import ToastHost from './components/ToastHost.vue'
import Icon from './components/Icon.vue'
import type { IconName } from './components/icons'

const store = useScanStore()
onMounted(() => store.bindEvents())

// 快捷键：Space 预览当前项 / Cmd(Ctrl)+A 全选 / Del 清除选择 / Esc 关闭浮层
function onKeydown(e: KeyboardEvent) {
  const inInput = ['INPUT', 'TEXTAREA', 'SELECT'].includes((e.target as HTMLElement)?.tagName ?? '')
  if (e.key === 'Escape') {
    if (store.preview) { store.preview = null; e.preventDefault(); return }
    if (store.failedOpen) { store.failedOpen = false; e.preventDefault(); return }
    return
  }
  if (inInput) return
  if (store.view === 'result' && !store.preview) {
    if (e.code === 'Space') {
      e.preventDefault()
      store.previewCurrent()
    } else if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'a') {
      e.preventDefault()
      store.selectAll()
    } else if (e.key === 'Delete' || e.key === 'Backspace') {
      e.preventDefault()
      store.clearSelection()
    }
  }
}
onMounted(() => window.addEventListener('keydown', onKeydown))
onUnmounted(() => window.removeEventListener('keydown', onKeydown))

// P2-1：导航图标由文字符号（◎ ⧉ ⚙）改为统一线性图标集的图标名。
const navs: { key: 'scan' | 'result' | 'settings'; label: string; icon: IconName }[] = [
  { key: 'scan', label: '扫描', icon: 'scan' },
  { key: 'result', label: '结果', icon: 'layers' },
  { key: 'settings', label: '设置', icon: 'gear' },
]
</script>

<template>
  <div class="layout">
    <aside class="sidebar">
      <div class="logo">FD</div>
      <nav>
        <button
          v-for="n in navs"
          :key="n.key"
          class="nav-btn"
          :class="{ active: store.view === n.key }"
          :title="n.label"
          @click="store.switchView(n.key)"
        >
          <Icon class="icon" :name="n.icon" :size="18" />
          <span class="label">{{ n.label }}</span>
          <span v-if="n.key === 'result' && store.failed.length" class="badge">{{
            store.failed.length
          }}</span>
        </button>
      </nav>
      <div class="ver">v{{ store.appVersion }}</div>
    </aside>

    <main class="main">
      <ScanView v-if="store.view === 'scan'" />
      <ResultView v-else-if="store.view === 'result'" />
      <SettingsView v-else />
    </main>

    <FailedDrawer />
    <PreviewPanel />
    <ToastHost />
  </div>
</template>

<style scoped>
.layout {
  display: flex;
  height: 100%;
}
.sidebar {
  width: 64px;
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 12px 0;
  gap: var(--sp-2);
  background: var(--bg-panel);
  border-right: 1px solid var(--border);
}
.logo {
  font-weight: 700;
  font-size: var(--fs-lg);
  color: var(--primary-ink);
  margin-bottom: 10px;
  letter-spacing: 1px;
}
nav {
  display: flex;
  flex-direction: column;
  gap: var(--sp-1);
  flex: 1;
}
.nav-btn {
  position: relative;
  width: 52px;
  padding: 8px 0 6px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 2px;
  border-radius: var(--r-md);
  color: var(--text-2);
  background: transparent;
}
.nav-btn:hover { background: var(--bg-hover); }
.nav-btn.active { background: var(--primary-weak); color: var(--primary-ink); }
/* P2-1：图标改为 SVG 后不再需要 font-size，只保证固定行盒，避免导航高度随字体度量抖动 */
.icon { display: block; height: 18px; }
.label { font-size: var(--fs-xs); }
.badge {
  position: absolute;
  top: 1px;
  right: 3px;
  /* P2-2：字号由 9px 提到 --fs-xs(11px)。9px 已在可读性下限之下，
     且是导航徽章里唯一承载数字的文本。盒高随之 14 → 17 以容纳新的字高。 */
  min-width: 17px;
  height: 17px;
  padding: 0 3px;
  border-radius: var(--r-md);
  background: var(--danger);
  color: var(--on-danger);
  font-size: var(--fs-xs);
  line-height: 17px;
  text-align: center;
}
.ver { font-size: var(--fs-xs); color: var(--text-3); }
.main { flex: 1; overflow: hidden; display: flex; flex-direction: column; }
</style>
