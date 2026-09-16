<script setup lang="ts">
// 操作确认对话框（M3-T06）：回收站/移动 一级确认；永久删除强制勾选"我已知晓"（02 决策 7）。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useScanStore } from '../stores/scan'
import { useToastStore } from '../stores/toast'
import { api } from '../wails'
import { humanBytes } from '../utils/format'

const props = defineProps<{
  kind: 'trash' | 'delete' | 'move' | 'hardlink'
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
    default: return { title: '硬链接合并', danger: false, desc: '冗余路径将替换为指向保留文件的硬链接（仅同卷可用）。' }
  }
})

const canConfirm = computed(() => {
  if (props.kind === 'delete' && !acknowledged.value) return false
  if (props.kind === 'move' && !moveTarget.value) return false
  return true
})

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
onMounted(() => window.addEventListener('keydown', onKeydown, true))
onUnmounted(() => window.removeEventListener('keydown', onKeydown, true))
</script>

<template>
  <div class="mask" @click.self="emit('close')">
    <div class="panel dlg">
      <div class="t" :class="{ danger: meta.danger }">{{ meta.title }}</div>
      <p class="d">{{ meta.desc }}</p>
      <div class="box">
        <div>将处理 <b>{{ store.selectedFiles.length }}</b> 个文件</div>
        <div>共 <b>{{ humanBytes(store.selectedBytes) }}</b> 空间可释放</div>
      </div>
      <div v-if="kind === 'move'" class="moverow">
        <button class="btn-ghost" :disabled="picking" @click="pickDir">选择目录</button>
        <input v-model="moveTarget" type="text" placeholder="目标目录" />
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
.dlg { width: 420px; padding: 22px; display: flex; flex-direction: column; gap: 12px; }
.t { font-size: 15px; font-weight: 600; }
.t.danger { color: var(--danger); }
.d { color: var(--text-2); }
.box { background: var(--bg-hover); border-radius: var(--radius); padding: 10px 14px; display: flex; gap: 24px; font-size: 12px; color: var(--text-2); }
.box b { color: var(--text); font-variant-numeric: tabular-nums; }
.moverow { display: flex; gap: 8px; }
.moverow input { flex: 1; }
.ack { display: flex; gap: 6px; align-items: center; color: var(--danger); font-size: 12px; }
.btns { display: flex; justify-content: flex-end; gap: 10px; margin-top: 4px; }
</style>
