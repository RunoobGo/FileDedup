// 轻量提示 store（Y6）：替代阻塞式 alert()。
// 非阻塞、可堆叠、自动消失、可手动关闭；错误统一经由 notifyError 归一化。
import { defineStore } from 'pinia'
import { ref } from 'vue'

export type ToastKind = 'error' | 'warn' | 'info' | 'success'

export interface Toast {
  id: number
  kind: ToastKind
  msg: string
  // head：这条是"别的内容的目录"（如 Warnings 摘要行），溢出时最后才丢它。见 push 的注释。
  head?: boolean
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

  // evictPick 挑一条该丢的：先丢非 head 里最旧的，全是 head 才退回丢最旧的 head
  // （必须有后一半，否则 head 投满时这个 while 循环永不收敛）。
  function evictPick(): number {
    const victim = toasts.value.find((t) => !t.head) ?? toasts.value[0]
    return victim.id
  }

  // push 投一条提示。opts.head 标的是"这条是别的内容的目录"：溢出时它最后才被丢。
  //
  // M80（04 §6.11 FE-7）：改前是无条件 `dismiss(toasts.value[0].id)`（丢最旧），而
  // ResultView 的 showWarnings 一次投 1 条摘要 + 至多 5 条明细 + 1 条溢出行 = 最多 7 条，
  // 于是摘要行**必然**是第一个被自己投出的明细挤掉的——用户点「查看」想先读到
  // "有 N 条、去哪儿看全"，实际只看见碎片。丢最旧这个策略本身是对的（新到的更该被看见），
  // 所以只给摘要破例，不提高上限（提高只是把翻车点往后推，明细照样淹掉摘要）。
  function push(msg: string, kind: ToastKind = 'error', ms = DEFAULT_MS, opts?: { head?: boolean }): number {
    if (!msg) return 0
    const id = ++seq
    toasts.value.push({ id, kind, msg, head: opts?.head })
    while (toasts.value.length > MAX_TOASTS) dismiss(evictPick())
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
