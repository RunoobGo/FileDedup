// C3 / R-前端-3（2026-09-24 第五轮审查）：busyReason 漏了 histLoading。
//
// 缺陷原形：busyReason() 只看 opsRunning / scanning。openHistory 在途（histLoading=true）
// 时它返回 null ⇒ busy=false ⇒ guard() 不拦 startScan/executeOp/undo/clearKeep/applyKeep。
// 用户从记录页点了"恢复"，载入回包还没回来，又去扫描页点扫描（或对结果发起清理）——
// 载入回包与新动作同时写同一批结果态。resultGen 只能弃写**陈旧回包**，挡不住"新动作
// 正在起跑"这一侧。histBusy 单列了 histLoading 给记录页行按钮，但 guard 这条总线漏了。
//
// 修法（Step 2 推荐项）：把 histLoading 并入 busyReason —— guard 覆盖的全部入口在载入
// 窗口内一律被挡，busyTip 也如实显示原因；histBusy = busy || histLoading 语义不变。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createPinia, setActivePinia } from 'pinia'

import { installWailsStub } from './harness.mjs'
import { useScanStore } from '../src/stores/scan'
import { useToastStore } from '../src/stores/toast'

function setup() {
  setActivePinia(createPinia())
  const stub = installWailsStub({
    StartScan: () => Promise.resolve(null),
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

test('R-前端-3：histLoading 在途时 busy 为真、busyTip 给出原因', () => {
  const t = setup()
  t.store.histLoading = true
  assert.equal(t.store.busy, true, '历史载入在途应计入 busy（否则 guard 不拦新动作）')
  assert.ok(t.store.busyTip.includes('历史'), `busyTip 应说明历史载入中，实得：${t.store.busyTip}`)
})

test('R-前端-3：histLoading 在途时 startScan 被 guard 挡下（不起跑、弹一条提示）', () => {
  const t = setup()
  t.store.roots.push('/vol/a') // 越过 startScan 的"无根早退"，真正走到 guard 这道门
  t.store.histLoading = true
  t.store.startScan()
  assert.equal(t.store.scanning, false, '载入窗口内不得起跑新扫描')
  assert.ok(t.toast.toasts.length >= 1, '被挡下应弹提示，不能静默')
})

test('R-前端-3：histLoading 落闸后 busy 复位（不得永久卡忙）', () => {
  const t = setup()
  t.store.histLoading = true
  assert.equal(t.store.busy, true)
  t.store.histLoading = false
  assert.equal(t.store.busy, false, 'histLoading 复位后 busy 必须跟着复位')
})
