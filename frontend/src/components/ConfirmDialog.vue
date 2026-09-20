<script setup lang="ts">
// 操作确认对话框（M3-T06）：回收站/移动 一级确认；永久删除强制勾选"我已知晓"（02 决策 7）。
// 2026-09-20：新增 symlink（跨卷软链接合并）。它与 hardlink 是同类操作但**风险不同**，
// 因此 desc 必须把差异写清楚（悬空风险），不能沿用硬链接的文案。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useScanStore } from '../stores/scan'
import { useToastStore } from '../stores/toast'
import { api, type OpKind } from '../wails'
import { humanBytes } from '../utils/format'
import { useModal } from '../composables/useModal'

const props = defineProps<{
  kind: OpKind
}>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'confirm', targetDir?: string): void }>()

const store = useScanStore()
const toast = useToastStore()
const acknowledged = ref(false)
const moveTarget = ref('')
const picking = ref(false)

const meta = computed(() => {
  switch (props.kind) {
    case 'trash': return { title: '移入回收站', danger: false, desc: '文件将移入系统回收站，可随时还原。' }
    case 'delete': return { title: '永久删除', danger: true, desc: '文件将被永久删除，此操作不可恢复！' }
    case 'move': return { title: '移动到指定目录', danger: false, desc: '文件将移动到目标目录（跨盘自动复制并校验）。' }
    case 'symlink': return {
      title: '跨卷软链接合并',
      danger: false,
      desc: '冗余路径将替换为指向保留文件的「软链接」，磁盘上只保留一份数据。' +
        '注意：软链接指向的是保留文件的**路径**——若保留文件被删除、移动，' +
        '或它所在的磁盘被拔出，这些路径就会失效（打不开）。' +
        '需要管理员权限（或已开启开发者模式）；同卷文件请优先用硬链接。',
    }
    default: return { title: '硬链接合并', danger: false, desc: '冗余路径将替换为指向保留文件的硬链接（仅同卷可用）。' }
  }
})

const canConfirm = computed(() => {
  // 命中数还没从后端回来时不给确认：此刻"将处理几个文件"是未知的，
  // 而确认框的全部意义就是在动手前把数量说准（AS-H6）。
  if (store.procCountPending) return false
  if (props.kind === 'delete' && !acknowledged.value) return false
  if (props.kind === 'move' && !moveTarget.value) return false
  return true
})

// 启用了处理策略且**已拿到后端真值**、且真的收窄了范围时，计数必须分两层显示。
//
// ★ 这里原先直接写 store.selectedFiles.length / store.selectedBytes。
// 处理策略上线后那两个数就不等于"将被处理的数量"了：用户勾了 40 项、
// 优先文件夹只覆盖 12 项，确认框若还说"将处理 40 个文件"，
// 他点下确认后只有 12 项被动，剩下的 28 项无声无息——这属于**计数说谎**，
// 比不做这个功能更糟。所以两个数都给出来，落差写在明面上。
//
// pending 时 procExcluded 恒为 0（真值未到位，无从判断有没有收窄），
// 所以 procFiltering 此时必为假——但"没显示落差"不等于"没收窄"，
// 必须走下面的"计算中"分支，否则会落到 v-else 那条"将处理 40 个文件"上，又是一句假话。
const narrowed = computed(() => store.procFiltering)
const procDirsTip = computed(() => store.procDirs.filter(d => d.trim()).join('、'))

async function pickDir() {
  picking.value = true
  try {
    moveTarget.value = await api.selectDirectory()
  } catch (e: any) {
    toast.notifyError('选择目录失败', e)
  } finally {
    picking.value = false
  }
}

function confirm() {
  emit('confirm', props.kind === 'move' ? moveTarget.value : undefined)
}

// Esc 关闭：与预览/失败抽屉一致（App.vue 快捷键不感知本对话框的局部状态）
function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    e.preventDefault()
    e.stopPropagation()
    emit('close')
  }
}
onMounted(() => {
  window.addEventListener('keydown', onKeydown, true)
  // 结果页的 debounce 预取可能还没跑完就弹了窗；这里兜一次，
  // key 已匹配时是空操作（不会多跑一次 RPC）。
  void store.ensureProcCounts()
})
onUnmounted(() => window.removeEventListener('keydown', onKeydown, true))

// P2-3：浮层语义 + 焦点管理。首焦点落在 DOM 序第一个可聚焦元素上 ——
// delete 场景是「我已知晓」勾选框，其余场景是「取消」，都不会默认停在破坏性按钮上。
const dlgRef = ref<HTMLElement | null>(null)
useModal(dlgRef)
</script>

<template>
  <div class="mask" @click.self="emit('close')">
    <div
      ref="dlgRef"
      class="panel dlg"
      role="dialog"
      aria-modal="true"
      aria-labelledby="confirm-dialog-title"
      tabindex="-1"
    >
      <div id="confirm-dialog-title" class="t" :class="{ danger: meta.danger }">{{ meta.title }}</div>
      <p class="d">{{ meta.desc }}</p>
      <div class="box">
        <!-- 命中数尚未从后端返回：既不说"将处理 N 项"，也不说 0 项，只说在算。
             结果页的操作按钮此时已经是灰的，走到这一步通常是 debounce 与弹窗
             竞速的极窄窗口；ensureProcCounts 会在挂载时立刻补一次，几毫秒后就换真数。 -->
        <template v-if="store.procCountPending">
          <div>正在核算实际处理范围（优先文件夹内的命中数尚未返回）…</div>
        </template>
        <template v-else-if="narrowed">
          <div>将处理 <b>{{ store.effectiveCount }}</b> 个文件（已勾选 {{ store.selectedFiles.length }} 项，其中 {{ store.procExcluded }} 项不在优先文件夹内，本次不改动）</div>
          <div>共 <b>{{ humanBytes(store.effectiveBytes) }}</b> 空间可释放</div>
        </template>
        <template v-else>
          <div>将处理 <b>{{ store.selectedFiles.length }}</b> 个文件</div>
          <div>共 <b>{{ humanBytes(store.selectedBytes) }}</b> 空间可释放</div>
        </template>
      </div>
      <p v-if="narrowed" class="proctip">
        处理范围受「优先处理的文件夹」限制（{{ procDirsTip }}）。
        保留策略仍然优先：保留项在任何情况下都不会被处理。
      </p>
      <div v-if="kind === 'move'" class="moverow">
        <button class="btn-ghost" :disabled="picking" @click="pickDir">选择目录</button>
        <input v-model="moveTarget" type="text" placeholder="目标目录" aria-label="移动目标目录" />
      </div>
      <label v-if="kind === 'delete'" class="ack">
        <input v-model="acknowledged" type="checkbox" />
        我已知晓此操作不可恢复，且不会经过回收站
      </label>
      <div class="btns">
        <button class="btn-ghost" @click="emit('close')">取消</button>
        <button class="btn-primary" :class="{ 'btn-danger': meta.danger }" :disabled="!canConfirm" @click="confirm">
          确认{{ meta.title }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.mask { position: fixed; inset: 0; background: rgba(0,0,0,0.4); display: flex; align-items: center; justify-content: center; z-index: 120; }
.dlg { width: 420px; padding: var(--sp-5); display: flex; flex-direction: column; gap: var(--sp-3); border-radius: var(--r-lg); }
.t { font-size: var(--fs-lg); font-weight: 600; }
.t.danger { color: var(--danger-ink); }
.d { color: var(--text-2); }
.box { background: var(--bg-hover); border-radius: var(--r-sm); padding: 10px 14px; display: flex; gap: var(--sp-5); font-size: var(--fs-sm); color: var(--text-2); }
.box b { color: var(--text); font-variant-numeric: tabular-nums; }
.proctip { color: var(--warn-ink, var(--text-2)); font-size: var(--fs-sm); margin: 0; }
.moverow { display: flex; gap: var(--sp-2); }
.moverow input { flex: 1; }
.ack { display: flex; gap: var(--sp-1); align-items: center; color: var(--danger-ink); font-size: var(--fs-sm); }
.btns { display: flex; justify-content: flex-end; gap: var(--sp-3); margin-top: var(--sp-1); }
</style>
