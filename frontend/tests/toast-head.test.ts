// §20 第九批 P-20-2（M80，04 §6.11 FE-7；设计段 §20.0-2 / §20.1-2）。
//
// 缺陷原形：toast.ts:38 溢出时 `dismiss(toasts.value[0].id)`——**丢最旧**；
// 而 ResultView.vue:63-68 的 showWarnings 一次投 1（摘要）+ 至多 5（明细）+ 1（溢出行）
// = 最多 7 条 ⇒ 当 Warnings ≥5 时，摘要行永远是第一个被自己投出的明细挤掉的。
// 用户点「查看」想先读到那句"有 N 条需要你关注"，实际看见的只有明细碎片。
//
// 修法只给摘要破例（head）：淘汰时先丢非 head 里最旧的，全是 head 才丢最旧的 head。
// ★ 改前多传的第 4 个参数会被 JS 直接忽略 ⇒ 本文件在改前树**能跑、且红在判据那一格**
//   （不是 import 失败那种"红错了地方"）。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createPinia, setActivePinia } from 'pinia'

import { installWailsStub } from './harness.mjs'
import { useToastStore } from '../src/stores/toast'

function setup() {
  setActivePinia(createPinia())
  const stub = installWailsStub({})
  return { toast: useToastStore(), stub }
}

const msgs = (t: { toasts: { msg: string }[] }) => t.toasts.map((x) => x.msg)

test('摘要行标 head 后，被 7 条明细挤也挤不掉，且可见总数仍不超上限', () => {
  const { toast, stub } = setup()
  toast.push('有 9 条需要你关注的提示，可悬停查看明细', 'error', 12000, { head: true })
  for (let i = 1; i <= 7; i++) toast.push(`明细 ${i}`, 'error', 12000)
  assert.ok(
    msgs(toast).includes('有 9 条需要你关注的提示，可悬停查看明细'),
    `摘要行被自己投出的明细挤掉了，用户看不见"有几条、去哪儿看"：实测 ${JSON.stringify(msgs(toast))}`,
  )
  assert.ok(toast.toasts.length <= 5, `可见提示数越过上限：${toast.toasts.length}`)
  stub.restore()
})

test('淘汰顺序：先丢非 head 里最旧的，head 之间仍按最旧先丢', () => {
  const { toast, stub } = setup()
  toast.push('摘要 A', 'error', 12000, { head: true })
  toast.push('摘要 B', 'error', 12000, { head: true })
  for (let i = 1; i <= 6; i++) toast.push(`明细 ${i}`, 'error', 12000)
  assert.deepEqual(msgs(toast), ['摘要 A', '摘要 B', '明细 4', '明细 5', '明细 6'])

  // 同一 store 实例（pinia 单例），清池再验第二格：全 head 时退回丢最旧。
  toast.toasts = []
  for (let i = 1; i <= 7; i++) toast.push(`摘要 ${i}`, 'error', 12000, { head: true })
  assert.deepEqual(
    msgs(toast),
    ['摘要 3', '摘要 4', '摘要 5', '摘要 6', '摘要 7'],
    '全是 head 时必须退回"丢最旧"，否则池子会无限涨',
  )
  stub.restore()
})

test('不传 head 时行为与改前一致（普通提示仍照旧丢最旧，防顺手改坏别的调用点）', () => {
  const { toast, stub } = setup()
  for (let i = 1; i <= 7; i++) toast.push(`普通 ${i}`, 'error', 12000)
  assert.deepEqual(msgs(toast), ['普通 3', '普通 4', '普通 5', '普通 6', '普通 7'])
  stub.restore()
})
