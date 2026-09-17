<script setup lang="ts">
// 扫描页：目录管理（拖拽/选择器/粘贴）+ 过滤器（渐进披露）+ 扫描进行态（M2-T04/T05）。
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useScanStore, emptyFilters } from '../stores/scan'
import { useToastStore } from '../stores/toast'
import { api } from '../wails'
import { humanBytes, humanSpeed, humanTime, statusLabel, stageLabel, formatCount } from '../utils/format'
import Icon from '../components/Icon.vue'

const store = useScanStore()
const toast = useToastStore()
const pastePath = ref('')
const advancedOpen = ref(false)

const stageText = computed(() => {
  const st = store.progress?.Stage
  return st ? (stageLabel[st] ?? st) : store.stageDesc
})

// P2-8①：运行态下 Status 与 Stage 是**同一个维度**，并排展示就成了同义重复
// （实测截图里出现「全量哈希」+「哈希中」两行近义文案）。
// 后端在运行时对每个阶段都有唯一对应的状态：
//   scan ↔ Scanning、prefilter ↔ Prefiltering、hash ↔ Hashing
// 因此当 Status 恰好是当前 Stage 的"进行时"表述时不再单独渲染状态标签；
// 只有 Status 携带阶段之外的信息（已暂停 / 空闲 / 已完成 / 已取消 / 失败，
// 或阶段与状态不同步）时才显示，避免丢掉真正的新信息。
const STAGE_OF_STATUS: Record<string, string> = {
  Scanning: 'scan',
  Prefiltering: 'prefilter',
  Hashing: 'hash',
}
const statusTag = computed(() => {
  const st = store.status
  const implied = STAGE_OF_STATUS[st]
  if (implied && implied === (store.progress?.Stage ?? '')) return ''
  return statusLabel[st] ?? st
})

async function addByPicker() {
  // C10：dev 模式（window.go 缺失）backend() 同步抛错，await 后须接住，否则 unhandled rejection
  try {
    const dir = await api.selectDirectory()
    if (dir) addRoot(dir)
  } catch (e: any) {
    toast.notifyError('选择目录失败', e)
  }
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
        <span v-if="statusTag" class="status-tag">{{ statusTag }}</span>
      </div>
      <div class="bar"><div class="fill" :style="{ width: progressPercent + '%' }"></div></div>
      <div class="stats">
        <div class="stat">
          <div class="v">{{ formatCount(store.progress?.FilesDone ?? 0) }}</div>
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
            <button class="x" aria-label="移除该目录" :title="`移除目录 ${r}`" @click="removeRoot(i)">
              <Icon name="close" :size="14" />
            </button>
          </div>
        </div>
        <!-- 虚线框只该圈住"拖放目标"。按钮与输入框是点击类控件，圈进去会让人误以为
             它们也属于可拖入区域（视觉上也把一处虚线框变成了整块内容的容器）。 -->
        <div class="add-row">
          <button class="btn-primary" @click="addByPicker">选择目录</button>
          <!-- P2-3：本输入框原先只有 placeholder，没有可访问名称（占位符不能当标签用，
               聚焦后即消失，且屏幕阅读器不会把它当名称播报）。补 aria-label。 -->
          <input
            v-model="pastePath"
            type="text"
            aria-label="粘贴要扫描的目录路径"
            placeholder="或粘贴路径后回车"
            @keydown.enter="addRoot(pastePath)"
          />
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
        <button class="toggle" :aria-expanded="advancedOpen" @click="advancedOpen = !advancedOpen">
          {{ advancedOpen ? '收起' : '展开' }}高级选项
          <Icon name="chevron-down" :size="14" :class="['chev', { open: advancedOpen }]" />
        </button>
        <div v-show="advancedOpen" class="grid adv">
          <label>包含扩展名<input v-model="extInclude" type="text" placeholder=".jpg,.png（空 = 全部）" /></label>
          <label>排除扩展名<input v-model="extExclude" type="text" placeholder=".tmp,.log" /></label>
          <label class="wide">排除路径（glob）<input v-model="excludePaths" type="text" placeholder="node_modules, build/**, *.tmp" /></label>
          <label>线程数<input v-model.number="store.threads" type="number" min="0" placeholder="0 = 自动（核数-1）" /></label>
        </div>
        <div class="filter-foot">
          <button class="btn-ghost" @click="resetFilters">重置过滤</button>
          <button class="btn-primary start"
            :disabled="store.roots.length === 0 || store.opsRunning"
            :title="store.opsRunning ? '清理操作执行中，请等待完成' : undefined"
            @click="store.startScan()">
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
  padding: var(--sp-5) var(--page-gutter);
  display: flex;
  flex-direction: column;
  gap: var(--sp-4);
}
.sec-title { font-weight: 600; margin-bottom: var(--sp-3); }

/* 运行态 */
.running { padding: var(--sp-5); display: flex; flex-direction: column; gap: var(--sp-4); }
.run-head { display: flex; align-items: center; gap: var(--sp-3); }
.dot {
  width: 10px; height: 10px; border-radius: 50%;
  background: var(--primary);
  animation: pulse 1.2s ease-in-out infinite;
}
/* 暂停态圆点属状态指示图形，需 ≥3:1（--warn 亮色仅 2.15:1） */
.dot.paused { background: var(--warn-ink); animation: none; }
@keyframes pulse { 50% { opacity: 0.3; } }
.stage { font-weight: 600; font-size: var(--fs-lg); }
.status-tag { color: var(--text-3); }
.bar { height: 8px; border-radius: var(--r-sm); background: var(--bg-hover); overflow: hidden; }
.fill { height: 100%; background: var(--primary); border-radius: var(--r-sm); transition: width 0.3s; }
.stats { display: grid; grid-template-columns: repeat(4, 1fr); gap: var(--sp-3); }
.stat { text-align: center; padding: 10px 0; border-radius: var(--r-md); background: var(--bg-hover); }
.stat .v { font-size: var(--fs-lg); font-weight: 600; font-variant-numeric: tabular-nums; }
.stat .k { font-size: var(--fs-sm); color: var(--text-3); margin-top: 2px; }
.actions { display: flex; gap: var(--sp-3); justify-content: center; }

/* 目录 */
/* 面板内边距与下方「过滤器」面板对齐。原先 .roots 没有 padding，
   「扫描目录」标题与虚线框都紧贴面板边框（实测各仅 1px），
   而同一页的过滤器面板是 16px —— 两块面板的节奏对不上，
   虚线框看起来像溢出了面板。 */
.roots { padding: var(--sp-4); }
.drop-zone { border: 1.5px dashed var(--border); border-radius: var(--r-md); padding: var(--sp-4); }
.empty { text-align: center; color: var(--text-3); padding: var(--sp-5) 0; }
.root-item {
  display: flex; align-items: center; gap: var(--sp-2);
  padding: 7px 10px; border-radius: var(--r-md); margin-bottom: 6px;
  background: var(--bg-hover); user-select: text;
}
/* 末项的 margin-bottom 会叠在虚线框的 padding 上，使框的下沿比上沿多 6px（不等距） */
.root-item:last-child { margin-bottom: 0; }
.path { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--mono); font-size: var(--fs-sm); }
/* P1-5：原 21.9×20，低于 28×28 的图标按钮下限。撑到 28×28 后负纵向 margin 抵消，
   目录行高保持 34px。 */
.x {
  background: none; color: var(--text-3);
  display: flex; align-items: center; justify-content: center;
  min-width: 28px; min-height: 28px; margin: -4px 0;
  padding: 0; border-radius: var(--r-md); line-height: 1;
}
.x:hover { color: var(--danger-ink); background: var(--danger-weak); }
/* 按钮与输入框已移出虚线框：与上方拖放区保持 12px，和「标题 → 拖放区」同一节奏 */
.add-row { display: flex; gap: var(--sp-3); margin-top: var(--sp-3); }
.add-row input { flex: 1; }

/* 过滤器 */
.filters { padding: var(--sp-4); }
.grid { display: grid; grid-template-columns: 1fr 1fr 1fr 1fr; gap: var(--sp-3); }
.grid label { display: flex; flex-direction: column; gap: var(--sp-1); font-size: var(--fs-sm); color: var(--text-2); }
.grid label.wide { grid-column: span 2; }
/* P0-1：`.check` 的 (0,1,0) 特异性压不过 `.grid label` 的 (0,1,1)，
   导致 flex-direction: row 从未生效、复选框与自己的标签错行 19px。
   用 `.grid label.check` 提高权重，并让「复选框 + 文案」与同格数字输入框落在同一水平带。
   偏移量推导：数字格控件顶部 = 标签行高 + gap；复选框比输入框矮 14px（16 vs 30），
   居中后需再下移 7px，故 padding-top = 标签行高 + gap。
   P1-7：把 gap 写进 calc，间隔令牌一改这里自动跟随，不再是一个孤立的魔数。
   标签行高为 12px 字体在 normal 行高下的实测值 17.5px。 */
.grid label.check {
  flex-direction: row;
  align-items: center;
  gap: var(--sp-1);
  padding-top: calc(17.5px + var(--sp-1));
}
.toggle { background: none; color: var(--primary-ink); padding: 8px 0 4px; text-align: left; }
/* P2-1：折叠指示由文字字符 ▾ 改为线性图标，展开态用旋转表达（原先只靠 ▾/▸ 两种字形） */
.toggle .chev { margin-left: 4px; transition: transform 0.18s ease; }
.toggle .chev.open { transform: rotate(180deg); }
.adv { border-top: 1px dashed var(--border); padding-top: 10px; }
.filter-foot { display: flex; justify-content: flex-end; gap: var(--sp-3); margin-top: 14px; }
.start { font-size: var(--fs-lg); padding: 9px 22px; }
</style>
