<script setup lang="ts">
// 应用骨架：侧边导航 + 主工作区（01 §6.2 布局）+ 全局快捷键（M4-T05）。
import { onMounted, onUnmounted } from 'vue'
import { useScanStore } from './stores/scan'
import ScanView from './views/ScanView.vue'
import ResultView from './views/ResultView.vue'
import SettingsView from './views/SettingsView.vue'
import FailedDrawer from './components/FailedDrawer.vue'
import PreviewPanel from './components/PreviewPanel.vue'

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

const navs = [
  { key: 'scan', label: '扫描', icon: '◎' },
  { key: 'result', label: '结果', icon: '⧉' },
  { key: 'settings', label: '设置', icon: '⚙' },
] as const
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
          <span class="icon">{{ n.icon }}</span>
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
  gap: 8px;
  background: var(--bg-panel);
  border-right: 1px solid var(--border);
}
.logo {
  font-weight: 700;
  font-size: 15px;
  color: var(--primary);
  margin-bottom: 10px;
  letter-spacing: 1px;
}
nav {
  display: flex;
  flex-direction: column;
  gap: 4px;
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
  border-radius: 8px;
  color: var(--text-2);
  background: transparent;
}
.nav-btn:hover { background: var(--bg-hover); }
.nav-btn.active { background: var(--primary-weak); color: var(--primary); }
.icon { font-size: 16px; line-height: 1; }
.label { font-size: 10.5px; }
.badge {
  position: absolute;
  top: 2px;
  right: 4px;
  min-width: 14px;
  height: 14px;
  padding: 0 3px;
  border-radius: 7px;
  background: var(--danger);
  color: #fff;
  font-size: 9px;
  line-height: 14px;
  text-align: center;
}
.ver { font-size: 10px; color: var(--text-3); }
.main { flex: 1; overflow: hidden; display: flex; flex-direction: column; }
</style>
