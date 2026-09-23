<script setup lang="ts">
// 设置页（M2-T08 / M4-T06）：后端 settings.json 单一事实源 + 缓存管理。
import { onMounted, ref } from 'vue'
import { useScanStore } from '../stores/scan'
import { useToastStore } from '../stores/toast'
import { api } from '../wails'
import type { Settings, CacheStats } from '../wails'
import { humanBytes, formatCount } from '../utils/format'
import Icon from '../components/Icon.vue'

const store = useScanStore()
const toast = useToastStore()
const draft = ref<Settings | null>(null)
const saved = ref(false)
const cacheStats = ref<CacheStats | null>(null)

async function refreshCache() {
  try { cacheStats.value = await api.cacheStats() } catch { cacheStats.value = null }
}
// R3-5：原先这里是一句原生 `confirm(...)`——全仓唯一残留。
// 原生弹窗的样式与键盘行为都不受本应用控制，且绕开了 useModal 收口过的那套
// （role/aria-modal/焦点归还原，P2-3）；在 webview 里它甚至可能被静默挡掉，
// 于是"点了清空缓存却没反应"。改用与 RecordsView 清空记录同一形状的**两段式就地确认**。
const confirmClearCache = ref(false)
async function clearCache() {
  if (!confirmClearCache.value) { confirmClearCache.value = true; return }
  confirmClearCache.value = false
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

// B3-3：store.saveSettings 现在会抛出失败。修正前它吞掉异常返回 undefined，
// 这里既把整个设置态赋空、又提示"已保存"——保存失败在界面上完全不可见。
async function save() {
  if (!draft.value) return
  try {
    const result = await store.saveSettings(draft.value)
    // 后端会归一取值（threads 钳制、theme 兜底），回填草稿使界面 = 实际生效值
    draft.value = JSON.parse(JSON.stringify(result))
    saved.value = true
    setTimeout(() => (saved.value = false), 1500)
  } catch (e: any) {
    toast.notifyError('保存设置失败', e)
  }
}
</script>

<template>
  <div class="settings-view">
    <div class="panel box">
      <div class="sec">外观</div>
      <!-- P2-3：`.row label` 是裸 label（既未包裹控件也无 for），与控件没有关联，
           控件因此没有可访问名称。给控件补 aria-label。 -->
      <div class="row">
        <label>主题</label>
        <select v-if="draft" v-model="draft.theme" aria-label="主题">
          <option value="system">跟随系统</option>
          <option value="light">亮色</option>
          <option value="dark">暗色</option>
        </select>
      </div>
      <div class="row">
        <label>语言</label>
        <select v-if="draft" v-model="draft.language" aria-label="语言">
          <option value="zh">中文</option>
          <option value="en">English（M5）</option>
        </select>
      </div>

      <div class="sec">扫描</div>
      <div class="row">
        <label>默认线程数</label>
        <input v-if="draft" v-model.number="draft.threads" type="number" min="0" aria-label="默认线程数" />
        <span class="hint">0 = 自动（逻辑核心数 - 1）</span>
      </div>

      <div class="sec">哈希缓存（二次扫描提速）</div>
      <div class="row">
        <label>缓存统计</label>
        <span v-if="cacheStats" class="hint">
          {{ formatCount(cacheStats.entries) }} 条目（含全量 {{ formatCount(cacheStats.withFull) }}）·
          {{ humanBytes(cacheStats.dbSizeBytes) }}
          <template v-if="cacheStats.lastEvicted"> · 累计淘汰 {{ formatCount(cacheStats.lastEvicted) }}</template>
        </span>
        <span v-else class="hint">不可用</span>
      </div>
      <div class="row">
        <label>管理</label>
        <button class="btn-ghost" @click="refreshCache">刷新统计</button>
        <!-- R3-5：两段式就地确认（第一下只把风险说清，第二下才动手）。
             清空缓存不毁数据，但会让下次扫描退回首次速度，值得一句确认。 -->
        <template v-if="!confirmClearCache">
          <button class="btn-ghost" @click="confirmClearCache = true">清空缓存</button>
        </template>
        <template v-else>
          <span class="hint">确认清空哈希缓存？下次扫描将退化为首次扫描速度。</span>
          <button class="btn-danger" @click="clearCache">确认清空</button>
          <button class="btn-ghost" @click="confirmClearCache = false">取消</button>
        </template>
      </div>

      <div class="sec">关于</div>
      <div class="row about">
        <div>FileDedup v{{ store.appVersion }}</div>
        <div class="hint">多阶段过滤 · xxHash64 预筛 · BLAKE3-256 · MIT License</div>
      </div>

      <div class="foot">
        <span v-if="saved" class="ok"><Icon name="check" :size="13" /> 已保存</span>
        <button class="btn-primary" :disabled="!draft" @click="save">保存设置</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.settings-view { flex: 1; overflow-y: auto; padding: var(--sp-5) var(--page-gutter); }
.box { max-width: 640px; margin-inline: auto; padding: var(--sp-5) var(--page-gutter); }
.sec { font-weight: 600; margin: var(--sp-4) 0 var(--sp-3); padding-top: 12px; border-top: 1px solid var(--border); }
.sec:first-child { margin-top: 0; padding-top: 0; border-top: none; }
.row { display: flex; align-items: center; gap: var(--sp-3); margin-bottom: var(--sp-3); }
.row label { width: 110px; color: var(--text-2); }
.hint { color: var(--text-3); font-size: var(--fs-sm); }
.row.disabled { opacity: 0.55; }
.about div { color: var(--text-2); }
.foot { display: flex; justify-content: flex-end; align-items: center; gap: var(--sp-3); margin-top: var(--sp-4); }
.ok { color: var(--success-ink); display: inline-flex; align-items: center; gap: 4px; }
</style>
