<script setup lang="ts">
// 预览面板（M2 基础版：文本/HEX/图片；M4 缩略图增强）。
import { useScanStore } from '../stores/scan'

const store = useScanStore()
</script>

<template>
  <transition name="fade">
    <div v-if="store.preview" class="mask" @click.self="store.preview = null">
      <div class="panel box">
        <div class="p-head">
          <span class="path" :title="store.preview.path">{{ store.preview.path }}</span>
          <button class="btn-ghost" @click="store.preview = null">关闭 (Esc)</button>
        </div>
        <div class="p-body">
          <img v-if="store.preview.kind === 'image'"
            :src="`data:${store.preview.mime};base64,${store.preview.content}`" alt="预览" />
          <pre v-else-if="store.preview.kind === 'text'">{{ store.preview.content }}</pre>
          <pre v-else-if="store.preview.kind === 'hex'" class="hex">{{ store.preview.content }}</pre>
          <div v-else class="empty">{{ store.preview.content || '二进制文件' }}</div>
        </div>
      </div>
    </div>
  </transition>
</template>

<style scoped>
.mask {
  position: fixed; inset: 0; background: rgba(0, 0, 0, 0.45);
  display: flex; align-items: center; justify-content: center; z-index: 110;
}
.box { width: 720px; max-width: 90vw; max-height: 80vh; display: flex; flex-direction: column; }
.p-head {
  display: flex; justify-content: space-between; align-items: center; gap: 10px;
  padding: 10px 14px; border-bottom: 1px solid var(--border);
}
.path { font-family: var(--mono); font-size: 11.5px; color: var(--text-2); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.p-body { overflow: auto; padding: 12px; }
.p-body img { max-width: 100%; max-height: 60vh; display: block; margin: 0 auto; }
pre { font-family: var(--mono); font-size: 12px; line-height: 1.55; white-space: pre-wrap; user-select: text; }
pre.hex { white-space: pre; font-size: 11.5px; }
.empty { text-align: center; color: var(--text-3); padding: 30px 0; }
</style>
