// C1（R-前端-1，2026-09-24 第五轮审查）：ops:filtered 的 toast 文案假归因。
//
// 缺陷原形：scan.ts 的 ops:filtered 处理器把命中数一律说成「M 项在优先文件夹内」。
// 但后端 `filtered = len(ProcessDirs)>0 || len(ExcludeDirs)>0`——只配「不处理」目录
// （黑名单）时也会发这个事件，而界面上根本不存在优先文件夹。用户只拉黑了几个目录，
// toast 却告诉他「N 项在优先文件夹内」，是无中生有的归因。
//
// 修法（Step 2 推荐项）：收口为中性句「其中 M 项在本次处理范围内」，与
// ResultView.vue:315「在处理范围内」/ :464 的现读字面对齐（判据唯一性纪律，
// 不在前端重新推导是哪把过滤器生效——那是后端判据的第二份实现，会漂移）。
// unmatched 那句保留「优先文件夹」措辞：unmatched 仅由 ProcessDirs 产生
// （黑名单不产出未命中目录），非空即证明确有优先文件夹，措辞准确。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createPinia, setActivePinia } from 'pinia'

import { installWailsStub } from './harness.mjs'
import { useScanStore } from '../src/stores/scan'
import { useToastStore } from '../src/stores/toast'

function setup() {
  setActivePinia(createPinia())
  const stub = installWailsStub({
    GetFailedItems: () => [],
    GetResultGroups: () => ({ groups: [], total: 0, totalReclaimable: 0 }),
    GetStatus: () => 'Idle',
    GetSettings: () => ({ theme: 'light' }),
    GetVersion: () => '0.5.0',
    GetStartupNotice: () => '',
    ListScanHistory: () => [],
  })
  const store = useScanStore()
  store.bindEvents()
  return { store, stub, toast: useToastStore() }
}

test('C1：仅黑名单收窄（unmatched 为空）时，文案不得出现「优先文件夹」', () => {
  const t = setup()
  // 黑名单收窄的形状：matched<selected，但没有未命中的优先文件夹（unmatched=[]）。
  t.stub.emit('ops:filtered', { matched: 2, selected: 3, unmatched: [] })

  assert.equal(t.toast.toasts.length, 1, '应弹一条 ops:filtered 说明')
  const msg = t.toast.toasts[0].msg
  assert.ok(
    !msg.includes('优先文件夹'),
    `只配黑名单时不得假归因到「优先文件夹」，实得：${msg}`,
  )
  // 中性句必须仍然把两个数摆出来（selected 与 matched 之差就是被拦下的项）。
  assert.ok(msg.includes('3'), `文案应含已选数 3：${msg}`)
  assert.ok(msg.includes('2'), `文案应含命中数 2：${msg}`)
  assert.ok(msg.includes('处理范围'), `文案应使用中性「处理范围」措辞：${msg}`)
})

test('C1：白名单未命中（unmatched 非空）时，仍点名优先文件夹（措辞准确、保留）', () => {
  const t = setup()
  t.stub.emit('ops:filtered', { matched: 1, selected: 2, unmatched: ['/vol/typo'] })

  assert.equal(t.toast.toasts.length, 1)
  const msg = t.toast.toasts[0].msg
  // unmatched 非空 ⇒ 确实配了优先文件夹，点名它是准确的，必须保留。
  assert.ok(msg.includes('/vol/typo'), `应点名未命中的优先文件夹：${msg}`)
  assert.ok(msg.includes('处理范围'), `命中数那句应使用中性措辞：${msg}`)
})
