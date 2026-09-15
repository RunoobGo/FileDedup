<script setup lang="ts">
// 扫描页：目录管理（拖拽/选择器/粘贴）+ 过滤器（渐进披露）+ 扫描进行态（M2-T04/T05）。
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useScanStore, emptyFilters } from '../stores/scan'
import { api } from '../wails'
import { humanBytes, humanSpeed, humanTime, statusLabel, stageLabel } from '../utils/format'

const store = useScanStore()
const pastePath = ref('')
const advancedOpen = ref(false)

const stageText = computed(() => {
  const st = store.progress?.Stage
  return st ? (stageLabel[st] ?? st) : store.stageDesc
})

async function addByPicker() {
  const dir = await api.selectDirectory()
  if (dir) addRoot(dir)
}
function addRoot(p: string) {
  p = p.trim()
  if (!p) return
  if (!store.roots.includes(p)) store.roots.push(p)
  pastePath.value = ''
}

// 拖入目录：Wails 原生路径（跨平台稳定；浏览器 dev 模式无 runtime 则不可用，
// 旧 dataTransfer File.path 方案在 WKWebView 不可靠，已弃用）
function onFileDrop(_x: number, _y: number, paths: string[]) {
  for (const p of paths) addRoot(p)
}
onMounted(() => {
  window.runtime?.OnFileDrop?.(onFileDrop, false) // false：全窗口接受，不限 CSS drop target
})
onUnmounted(() => {
  window.runtime?.OnFileDropOff?.()
})
function removeRoot(i: number) { store.roots.splice(i, 1) }

const extInclude = computed({
  get: () => store.filters.IncludeExts.join(','),
  set: (v) => { store.filters.IncludeExts = v.split(',').map(s => s.trim()).filter(Boolean) },
})
const extExclude = computed({
  get: () => store.filters.ExcludeExts.join(','),
  set: (v) => { store.filters.ExcludeExts = v.split(',').map(s => s.trim()).filter(Boolean) },
})
const excludePaths = computed({
  get: () => store.filters.ExcludePaths.join(','),
  set: (v) => { store.filters.ExcludePaths = v.split(',').map(s => s.trim()).filter(Boolean) },
})

function resetFilters() {
  Object.assign(store.filters, emptyFilters())
}

const progressPercent = computed(() => {
  const p = store.progress
  if (!p || !p.BytesTotal) return 0
  return Math.min(100, (p.BytesDone / p.BytesTotal) * 100)
})
</script>

<template>
  <div class="scan-view">
    <!-- 扫描进行态 -->
    <div v-if="store.scanning" class="panel running">
      <div class="run-head">
        <span class="dot" :class="{ paused: store.status === 'Paused' }"></span>
        <span class="stage">{{ stageText }}</span>
        <span class="status-tag">{{ statusLabel[store.status] ?? store.status }}</span>
      </div>
      <div class="bar"><div class="fill" :style="{ width: progressPercent + '%' }"></div></div>
      <div class="stats">
        <div class="stat">
          <div class="v">{{ store.progress?.FilesDone ?? 0 }}</div>
          <div class="k">已处理文件</div>
        </div>
        <div class="stat">
          <div class="v">{{ humanBytes(store.progress?.BytesDone ?? 0) }}</div>
          <div class="k">已读数据</div>
        </div>
        <div class="stat">
          <div class="v">{{ humanSpeed(store.progress?.SpeedBps ?? 0) }}</div>
          <div class="k">速度</div>
        </div>
        <div class="stat">
          <div class="v">{{ humanTime(store.progress?.ETASeconds ?? -1) }}</div>
          <div class="k">预计剩余</div>
        </div>
      </div>
      <div class="actions">
        <button v-if="store.status !== 'Paused'" class="btn-ghost" @click="store.pauseScan()">暂停</button>
        <button v-else class="btn-primary" @click="store.resumeScan()">继续</button>
        <button class="btn-ghost" @click="store.cancelScan()">取消</button>
      </div>
    </div>

    <!-- 配置态 -->
    <template v-else>
      <div class="panel roots">
        <div class="sec-title">扫描目录</div>
        <div class="drop-zone" title="拖入文件夹">
          <div v-if="store.roots.length === 0" class="empty">将文件夹拖到这里，或点击下方添加</div>
          <div v-for="(r, i) in store.roots" :key="r" class="root-item">
            <span class="path" :title="r">{{ r }}</span>
            <button class="x" @click="removeRoot(i)">✕</button>
          </div>
          <div class="add-row">
            <button class="btn-primary" @click="addByPicker">选择目录</button>
            <input v-model="pastePath" type="text" placeholder="或粘贴路径后回车"
              @keydown.enter="addRoot(pastePath)" />
          </div>
        </div>
      </div>

      <div class="panel filters">
        <div class="sec-title">过滤器</div>
        <div class="grid">
          <label>最小大小 (KB)<input v-model.number="store.filters.MinSize" type="number" min="0" /></label>
          <label>最大大小 (KB)<input v-model.number="store.filters.MaxSize" type="number" min="0" placeholder="0 = 不限" /></label>
          <label class="check">
            <input v-model="store.filters.IncludeHidden" type="checkbox" />
            包含隐藏文件
          </label>
          <label class="check">
            <input v-model="store.paranoid" type="checkbox" />
            逐字节确认（paranoid）
          </label>
        </div>

        <!-- 渐进披露：高级选项折叠区（02 §5.2） -->
        <button class="toggle" @click="advancedOpen = !advancedOpen">
          {{ advancedOpen ? '收起' : '展开' }}高级选项 ▾
        </button>
        <div v-show="advancedOpen" class="grid adv">
          <label>包含扩展名<input v-model="extInclude" type="text" placeholder=".jpg,.png（空 = 全部）" /></label>
          <label>排除扩展名<input v-model="extExclude" type="text" placeholder=".tmp,.log" /></label>
          <label class="wide">排除路径（glob）<input v-model="excludePaths" type="text" placeholder="node_modules, build/**, *.tmp" /></label>
          <label>线程数<input v-model.number="store.threads" type="number" min="0" placeholder="0 = 自动（核数-1）" /></label>
        </div>
        <div class="filter-foot">
          <button class="btn-ghost" @click="resetFilters">重置过滤</button>
          <button class="btn-primary start" :disabled="store.roots.length === 0" @click="store.startScan()">
            开始扫描（{{ store.roots.length }} 个目录）
          </button>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.scan-view {
  flex: 1;
  overflow-y: auto;
  padding: 20px 24px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.sec-title { font-weight: 600; margin-bottom: 10px; }

/* 运行态 */
.running { padding: 26px; display: flex; flex-direction: column; gap: 16px; }
.run-head { display: flex; align-items: center; gap: 10px; }
.dot {
  width: 10px; height: 10px; border-radius: 50%;
  background: var(--primary);
  animation: pulse 1.2s ease-in-out infinite;
}
.dot.paused { background: var(--warn); animation: none; }
@keyframes pulse { 50% { opacity: 0.3; } }
.stage { font-weight: 600; font-size: 14px; }
.status-tag { color: var(--text-3); }
.bar { height: 8px; border-radius: 4px; background: var(--bg-hover); overflow: hidden; }
.fill { height: 100%; background: var(--primary); border-radius: 4px; transition: width 0.3s; }
.stats { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px; }
.stat { text-align: center; padding: 10px 0; border-radius: var(--radius); background: var(--bg-hover); }
.stat .v { font-size: 15px; font-weight: 600; font-variant-numeric: tabular-nums; }
.stat .k { font-size: 11px; color: var(--text-3); margin-top: 2px; }
.actions { display: flex; gap: 10px; justify-content: center; }

/* 目录 */
.drop-zone { border: 1.5px dashed var(--border); border-radius: var(--radius); padding: 14px; }
.empty { text-align: center; color: var(--text-3); padding: 22px 0; }
.root-item {
  display: flex; align-items: center; gap: 8px;
  padding: 7px 10px; border-radius: 6px; margin-bottom: 6px;
  background: var(--bg-hover); user-select: text;
}
.path { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--mono); font-size: 12px; }
.x { background: none; color: var(--text-3); padding: 2px 6px; }
.x:hover { color: var(--danger); }
.add-row { display: flex; gap: 10px; margin-top: 8px; }
.add-row input { flex: 1; }

/* 过滤器 */
.filters { padding: 18px; }
.grid { display: grid; grid-template-columns: 1fr 1fr 1fr 1fr; gap: 10px; }
.grid label { display: flex; flex-direction: column; gap: 5px; font-size: 12px; color: var(--text-2); }
.grid label.wide { grid-column: span 2; }
.check { flex-direction: row; align-items: center; gap: 6px; }
.toggle { background: none; color: var(--primary); padding: 8px 0 4px; text-align: left; }
.adv { border-top: 1px dashed var(--border); padding-top: 10px; }
.filter-foot { display: flex; justify-content: flex-end; gap: 10px; margin-top: 14px; }
.start { font-size: 14px; padding: 9px 22px; }
</style>
