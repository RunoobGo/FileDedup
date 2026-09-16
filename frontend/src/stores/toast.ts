// 轻量提示 store（Y6）：替代阻塞式 alert()。
// 非阻塞、可堆叠、自动消失、可手动关闭；错误统一经由 notifyError 归一化。
import { defineStore } from 'pinia'
import { ref } from 'vue'

export type ToastKind = 'error' | 'warn' | 'info' | 'success'

export interface Toast {
  id: number
  kind: ToastKind
  msg: string
}

// 同时最多展示的提示数（超出丢弃最旧，防刷屏淹没界面）
const MAX_TOASTS = 5
// 默认驻留时长（错误信息需要更长的阅读时间）
const DEFAULT_MS = 6000

let seq = 0

export const useToastStore = defineStore('toast', () => {
  const toasts = ref<Toast[]>([])
  const timers = new Map<number, ReturnType<typeof setTimeout>>()

  function dismiss(id: number) {
    toasts.value = toasts.value.filter((t) => t.id !== id)
    const h = timers.get(id)
    if (h !== undefined) {
      clearTimeout(h)
      timers.delete(id)
    }
  }

  function push(msg: string, kind: ToastKind = 'error', ms = DEFAULT_MS): number {
    if (!msg) return 0
    const id = ++seq
    toasts.value.push({ id, kind, msg })
    while (toasts.value.length > MAX_TOASTS) dismiss(toasts.value[0].id)
    timers.set(
      id,
      setTimeout(() => dismiss(id), ms),
    )
    return id
  }

  // errText 归一化后端/前端异常：Wails 返回字符串、Error 或 { error } 对象
  function errText(e: unknown): string {
    if (e === null || e === undefined) return ''
    if (typeof e === 'string') return e
    if (e instanceof Error) return e.message
    if (typeof e === 'object' && 'error' in (e as Record<string, unknown>)) {
      return String((e as Record<string, unknown>).error)
    }
    return String(e)
  }

  // notifyError 统一错误入口：title + ': ' + 详情
  function notifyError(title: string, e?: unknown) {
    const detail = errText(e)
    push(detail ? `${title}：${detail}` : title, 'error')
  }

  function notifySuccess(msg: string) {
    push(msg, 'success')
  }

  return { toasts, push, dismiss, notifyError, notifySuccess, errText }
})
