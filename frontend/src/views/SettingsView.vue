<script setup lang="ts">
// 设置页（M2-T08 / M4-T06）：后端 settings.json 单一事实源 + 缓存管理。
import { onMounted, ref } from 'vue'
import { useScanStore } from '../stores/scan'
import { useToastStore } from '../stores/toast'
import { api } from '../wails'
import type { Settings, CacheStats } from '../wails'
import { humanBytes } from '../utils/format'

const store = useScanStore()
const toast = useToastStore()
const draft = ref<Settings | null>(null)
const saved = ref(false)
const cacheStats = ref<CacheStats | null>(null)

async function refreshCache() {
  try { cacheStats.value = await api.cacheStats() } catch { cacheStats.value = null }
}
async function clearCache() {
  if (!confirm('确认清空哈希缓存？下次扫描将退化为首次扫描速度。')) return
  try {
    await api.cacheClear()
    toast.notifySuccess('哈希缓存已清空')
  } catch (e: any) {
    toast.notifyError('清空缓存失败', e)
  }
  await refreshCache()
}

onMounted(async () => {
  refreshCache()
  try {
    if (!store.settings) {
      const m = await import('../wails')
      store.settings = await m.api.getSettings()
    }
    draft.value = JSON.parse(JSON.stringify(store.settings))
  } catch {
    draft.value = null
  }
})

async function save() {
  if (!draft.value) return
  store.settings = await store.saveSettings(draft.value)
  saved.value = true
  setTimeout(() => (saved.value = false), 1500)
}
</script>

<template>
  <div class="settings-view">
    <div class="panel box">
      <div class="sec">外观</div>
      <div class="row">
        <label>主题</label>
        <select v-if="draft" v-model="draft.theme">
          <option value="system">跟随系统</option>
          <option value="light">亮色</option>
          <option value="dark">暗色</option>
        </select>
      </div>
      <div class="row">
        <label>语言</label>
        <select v-if="draft" v-model="draft.language">
          <option value="zh">中文</option>
          <option value="en">English（M5）</option>
        </select>
      </div>

      <div class="sec">扫描</div>
      <div class="row">
        <label>默认线程数</label>
        <input v-if="draft" v-model.number="draft.threads" type="number" min="0" />
        <span class="hint">0 = 自动（逻辑核心数 - 1）</span>
      </div>

      <div class="sec">哈希缓存（二次扫描提速）</div>
      <div class="row">
        <label>缓存统计</label>
        <span v-if="cacheStats" class="hint">
          {{ cacheStats.entries }} 条目（含全量 {{ cacheStats.withFull }}）·
          {{ humanBytes(cacheStats.dbSizeBytes) }}
          <template v-if="cacheStats.lastEvicted"> · 累计淘汰 {{ cacheStats.lastEvicted }}</template>
        </span>
        <span v-else class="hint">不可用</span>
      </div>
      <div class="row">
        <label>管理</label>
        <button class="btn-ghost" @click="refreshCache">刷新统计</button>
        <button class="btn-ghost" @click="clearCache">清空缓存</button>
      </div>

      <div class="sec">关于</div>
      <div class="row about">
        <div>FileDedup v{{ store.appVersion }}</div>
        <div class="hint">多阶段过滤 · xxHash64 预筛 · BLAKE3-256 · MIT License</div>
      </div>

      <div class="foot">
        <span v-if="saved" class="ok">已保存 ✓</span>
        <button class="btn-primary" :disabled="!draft" @click="save">保存设置</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.settings-view { flex: 1; overflow-y: auto; padding: 20px 24px; }
.box { max-width: 640px; padding: 20px 24px; }
.sec { font-weight: 600; margin: 16px 0 10px; padding-top: 12px; border-top: 1px solid var(--border); }
.sec:first-child { margin-top: 0; padding-top: 0; border-top: none; }
.row { display: flex; align-items: center; gap: 12px; margin-bottom: 10px; }
.row label { width: 110px; color: var(--text-2); }
.hint { color: var(--text-3); font-size: 12px; }
.row.disabled { opacity: 0.55; }
.about div { color: var(--text-2); }
.foot { display: flex; justify-content: flex-end; align-items: center; gap: 12px; margin-top: 18px; }
.ok { color: var(--success); }
</style>
