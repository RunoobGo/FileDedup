// FE-3（2026-09-21 全量审查）：历史列表的刷新没有代际复核，与结果集 B3-2 同源
// 的那条纪律漏了这一处。
//
// 缺陷原形：refreshHistory() 直接 `histList.value = await api.listScanHistory()`。
// 两次刷新可以在途重叠——onMounted 起一次、删除/清空后再起一次，而后回包
// 到达顺序没有任何保证。先发的请求晚回来时，它带着**删除前的列表**整体覆盖
// histList：用户刚删掉的那一行原地复活，再点一次删除又没了。列表看着"抽风"，
// 而账本其实已经删干净了——这是"界面说谎"的一类。
//
// 同一条缺陷在结果集上早就修过（B3-2：resultGen / bumpResultGen，见 scan.ts:142），
// 所以这里不是新发明，是把同一把锁补到漏掉的那扇门上。
//
// ★ 为什么用 deferred 而不是直接 await：竞态的本质是**回包顺序**，
//   顺序必须由测试握在手里；用 flush 碰运气等于没测。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createPinia, setActivePinia } from 'pinia'

import { installWailsStub, flush, deferred } from './harness.mjs'
import { useScanStore } from '../src/stores/scan'

const meta = (id: number) => ({
  id, savedAt: id, roots: [`/vol${id}`], groups: 1, files: 2, origFiles: 2,
  reclaimable: 10, threads: 2, paranoid: false, filters: {},
})

function setup(responders: Record<string, unknown> = {}) {
  setActivePinia(createPinia())
  const stub = installWailsStub({
    GetResultGroups: () => ({ groups: [], total: 0, totalReclaimable: 0 }),
    GetFailedItems: () => [],
    GetStatus: () => 'Idle',
    GetSettings: () => ({ theme: 'light' }),
    GetVersion: () => '0.5.0',
    GetStartupNotice: () => '',
    ...responders,
  })
  const store = useScanStore()
  return { store, stub }
}

const ids = (list: any[]) => list.map((m) => m.id)

test('两次刷新重叠：先发后回的陈旧列表不得覆盖后发的结果', async () => {
  const first = deferred()
  const second = deferred()
  let n = 0
  const { store, stub } = setup({
    ListScanHistory: () => (++n === 1 ? first.promise : second.promise),
  })

  const p1 = store.refreshHistory()
  const p2 = store.refreshHistory()

  // 后发的先回：列表应停在 [2]
  second.resolve([meta(2)])
  await p2
  assert.deepEqual(ids(store.histList), [2], '第二次刷新应已写入自己的结果')

  // 先发的那条带着"删除前的两行"晚回来 —— 必须作废
  first.resolve([meta(1), meta(2)])
  await p1
  await flush()
  assert.deepEqual(ids(store.histList), [2],
    '陈旧回包把列表倒回了删除前（已删的行会复活）')
  stub.restore()
})

test('删除后刷新：在途的旧列表不得让已删行复活（用户可见症状）', async () => {
  const stale = deferred()
  const fresh = deferred()
  let n = 0
  const { store, stub } = setup({
    // 第 1 次是 onMounted 那趟（在途未回），第 2 次是删除后补的那趟
    ListScanHistory: () => (++n === 1 ? stale.promise : fresh.promise),
    DeleteScanHistory: () => ({}),
  })

  const pMount = store.refreshHistory()
  const pDel = store.deleteHistory(7)
  await flush()

  fresh.resolve([meta(8)]) // 删除后的真列表：7 已不在
  await pDel
  assert.deepEqual(ids(store.histList), [8])

  stale.resolve([meta(7), meta(8)]) // 删除前抓取的列表现在才回
  await pMount
  await flush()
  assert.deepEqual(ids(store.histList), [8],
    '已删除的历史行被陈旧回包写回界面')
  stub.restore()
})

test('负控制：单次刷新仍照常写入（代际复核不得把正常路径一起挡掉）', async () => {
  const { store, stub } = setup({ ListScanHistory: () => [meta(1), meta(2)] })
  await store.refreshHistory()
  assert.deepEqual(ids(store.histList), [1, 2])
  // 连续两次、各自顺序返回（现实里最常见的那条路）：后一次必须生效
  store.refreshHistory()
  await flush()
  assert.equal(stub.invoked('ListScanHistory'), 2)
  assert.deepEqual(ids(store.histList), [1, 2])
  stub.restore()
})

test('histBusy 覆盖"正在载入历史"窗口：此时重扫/删除必须一起禁用', async () => {
  const load = deferred()
  const { store, stub } = setup({
    ListScanHistory: () => [meta(1)],
    LoadScanHistory: () => load.promise,
    GetResultGroups: () => ({ groups: [], total: 0, totalReclaimable: 0 }),
  })
  await store.refreshHistory()
  assert.equal(store.histBusy, false, '空闲时不得把按钮钉死')

  const p = store.openHistory(1)
  await flush()
  assert.equal(store.histBusy, true,
    '载入回包期间 histBusy 必须为 true（FE-3：重扫/删除原先不受这条控制）')

  load.resolve({ groups: 0, reclaimable: 0, durationMs: 0, filesTotal: 0 })
  await p
  await flush()
  assert.equal(store.histBusy, false, '载入完成后必须恢复可点')
  stub.restore()
})

test('histBusy 与扫描/清理互斥同源：busy 为真时它也必须为真', async () => {
  const { store, stub } = setup({ ListScanHistory: () => [] })
  store.opsRunning = true
  assert.equal(store.histBusy, true, '清理在途时历史行动作必须禁用')
  store.opsRunning = false
  store.scanning = true
  assert.equal(store.histBusy, true, '扫描在途时历史行动作必须禁用')
  store.scanning = false
  assert.equal(store.histBusy, false)
  stub.restore()
})
