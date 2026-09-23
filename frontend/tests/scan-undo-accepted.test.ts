// R3-4（2026-09-23 第四轮全仓审查，设计段 §30.9 ②）：回撤的"受理与否"必须是返回值。
//
// 缺陷原形（`RecordsView.vue:122-124` + `scan.ts:459-470`）：
//   function undoOne(m, it) {
//     undoingItem.value = it.id      ← 先乐观置位
//     store.undoItem(m.id, it.id)    ← 返回 undefined，视图无从知道有没有被受理
//   }
// 而 `undoItem` 第一句是 `if (!guard('回撤')) return`——被互斥挡下时**不置** opsRunning，
// 于是视图那条"下降沿清标记"的 watch（RecordsView:138-143）永远不会触发，
// 该行标签永久停在"执行中…"（:324），而 `canUndoItem` 仍为真 —— 看着像一个卡死的进行中。
//
// ★ 为什么让 store 回答"受理了没有"，而不是视图先问 `store.busy`：
//   `busy` 与 `guard` 同源，但视图自行取用等于在**第二处**引用这条判据；
//   `guard` 将来多加一条理由（例如"历史回包在途"）视图就会漏。判据留在 store 一份。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createPinia, setActivePinia } from 'pinia'

import { installWailsStub } from './harness.mjs'
import { useScanStore } from '../src/stores/scan'
import { useToastStore } from '../src/stores/toast'

function setup() {
  setActivePinia(createPinia())
  const stub = installWailsStub({
    UndoOperation: () => 'ok',
    UndoOperationItem: () => 'ok',
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
  return { store, stub }
}

test('受理时返回 true，并真的发出 RPC（单条）', async () => {
  const { store, stub } = setup()
  assert.equal(await store.undoItem(7, 42), true)
  assert.equal(stub.invoked('UndoOperationItem'), 1)
  stub.restore()
})

test('被互斥挡下时返回 false，且一个 RPC 都不发（单条）', async () => {
  const { store, stub } = setup()
  store.opsRunning = true // 账本在途：guard 的判据就是这一条
  assert.equal(await store.undoItem(7, 42), false)
  assert.equal(stub.invoked('UndoOperationItem'), 0)
  assert.equal(store.opsRunning, true, '被拒那一次不得解开别人的互斥标记')
  stub.restore()
})

test('整条记录的回撤同样回答"受理了没有"（批量腿同契约）', async () => {
  const { store, stub } = setup()
  assert.equal(await store.undoRecord(7), true, '受理腿')
  store.opsRunning = true
  assert.equal(await store.undoRecord(7), false, '被拒腿')
  assert.equal(stub.invoked('UndoOperation'), 1, '被拒那一次不得发出第二次')
  stub.restore()
})

test('RPC 抛错时仍是"已受理"（收尾归 opsRunning 下降沿，不是返回值）', async () => {
  // 返回值只回答"要不要开始做"。开始之后失败，视图靠下降沿复位——
  // 若这里返回 false，视图会把标签收回成"回撤"，用户以为什么都没发生。
  setActivePinia(createPinia())
  const stub = installWailsStub({
    UndoOperationItem: () => Promise.reject(new Error('保留文件已不存在，无法放回')),
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
  assert.equal(await store.undoItem(7, 42), true, '失败是"做过且没做成"，不是"没受理"')
  assert.equal(store.opsRunning, false, '失败腿必须自己解开互斥')
  assert.equal(useToastStore().toasts.length, 1, '失败要看得见')
  stub.restore()
})
