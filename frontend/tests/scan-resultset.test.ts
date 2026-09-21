// M17（2026-09-21 全仓审计 §五 17）：结果集换代之际，视图态必须跟着换代。
//
// 钉两件事：
//   1) 扩展名过滤**不跨结果集**存活（新扫描、打开历史两条入口都要归零）。
//      危害不是"数字对不上"这么简单：统计条与工具栏整块挂在 totalGroups > 0 上，
//      而 totalGroups 是**过滤后**的口径——过滤到新结果集一条都不剩时，
//      那个还在生效的过滤框会连同统计条一起消失，用户看到"没有发现重复文件"，
//      既不知道自己在过滤、也没有地方取消。
//   2) 排序是**偏好**不是过滤：它只改顺序、不藏组，跨结果集保留才是对用户有利的行为。
//      这条刻意写成反向断言，防止有人把 resultSort 也一起清了。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createPinia, setActivePinia } from 'pinia'

import { installWailsStub, flush } from './harness.mjs'
import { useScanStore } from '../src/stores/scan'

function setup() {
  setActivePinia(createPinia())
  const stub = installWailsStub({
    StartScan: () => Promise.resolve('scan-1'),
    GetResultGroups: () => ({ groups: [], total: 0, totalReclaimable: 0 }),
    GetFailedItems: () => [],
    GetStatus: () => 'Idle',
    GetSettings: () => ({ theme: 'light' }),
    GetVersion: () => '0.5.0',
    GetStartupNotice: () => '',
    ListScanHistory: () => [{ id: 77, roots: ['/a'], savedAt: 1, groups: 5, reclaimable: 555 }],
    LoadScanHistory: () => ({ groups: 5, reclaimable: 555, durationMs: 3, filesTotal: 20 }),
  })
  const store = useScanStore()
  store.bindEvents()
  return { store, stub }
}

/** 最近一次 GetResultGroups 的查询参数（后端按 ext/sort 取页）。 */
const lastQuery = (stub: any) =>
  [...stub.calls].reverse().find((c: any) => c.method === 'GetResultGroups')?.args?.[0]

test('打开历史：扩展名过滤归零，排序保留', async () => {
  const { store, stub } = setup()
  store.resultExt = '.jpg'
  store.resultSort = 'size'
  await store.openHistory(77)
  await flush()

  assert.equal(store.resultExt, '', '过滤必须随结果集换代')
  assert.equal(lastQuery(stub).ext, '', '取页请求不得带上上一轮的过滤')
  assert.equal(store.resultSort, 'size', '排序是偏好，跨结果集保留')
  assert.equal(lastQuery(stub).sort, 'size')
  // 注：histResult 关联靠 histList（由历史页的刷新动作填充），本桩具里它是空的，
  // 与 M17 无关，故不断言。
  stub.restore()
})

test('新扫描：扩展名过滤归零（clearStaleResult 这条路径同样要覆盖）', async () => {
  const { store, stub } = setup()
  store.resultExt = '.png'
  store.roots = ['/tmp/whatever']
  store.startScan()
  await flush()
  stub.emit('scan:done', { groups: 5, reclaimable: 555, durationMs: 3, filesTotal: 20 })
  await flush()

  assert.equal(store.resultExt, '')
  assert.equal(lastQuery(stub).ext, '')
  assert.equal(store.histResult, null, '新扫描与旧历史解绑')
  stub.restore()
})

test('反向哨兵：过滤框本身在归零前确实会改变取页参数（证明这条判据不是空转）', async () => {
  const { store, stub } = setup()
  store.resultExt = '.gif'
  await store.loadResultPage(false) // 同一结果集内改过滤：必须真的传给后端
  assert.equal(lastQuery(stub).ext, '.gif')
  stub.restore()
})
