// M16（2026-09-21 全仓审计 §五 16）：startScan 的清态与 scan:done 事件竞速。
//
// 缺陷原形：`.then(clearStaleResult)` 无条件执行。小目录 + 缓存命中时后端可以在
// StartScan 的 RPC 回包**之前**就跑完并发出 scan:done，done 处理器刚装进新结果，
// 这句清态又把它整体抹掉 → 界面停在"暂无结果"，数据其实完好躺在历史里。
//
// 修正不是"跳过清态"：上一轮的勾选 / opsResult / 优先文件夹必须作废，
// 否则界面上残留的 fileID 已失效，勾上去就能对错文件发起清理（那才是更坏的结果）。
// 所以是"照旧清、清完把本次收尾重放一次"。三条用例各钉一个方向：
//   1) done 先到（缺陷路径）：清态生效 + 新结果被重放回界面
//   2) 回包先到（主路径）：行为不变，且**不该**多做一次取页
//   3) 第二轮扫描：重放必须限定在"本次真的收到过 done"，不能借用上一轮的摘要
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createPinia, setActivePinia } from 'pinia'

import { installWailsStub, flush, deferred } from './harness.mjs'
import { useScanStore } from '../src/stores/scan'

const PAGED = {
  groups: [{ id: 11, count: 3, size: 4096, reclaimable: 8192, files: [] }],
  total: 7,
  totalReclaimable: 12345,
}

const DONE = { groups: 7, reclaimable: 999, durationMs: 5, filesTotal: 100 }

/** 起一个真 store：桩好后端 + 绑定事件。StartScan 的回复由调用方控制时序。 */
function setup(startScanResponder: () => unknown) {
  setActivePinia(createPinia())
  const stub = installWailsStub({
    StartScan: startScanResponder,
    GetResultGroups: () => structuredClone(PAGED),
    GetFailedItems: () => [],
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

test('done 早于 StartScan 回包：清态之后必须把新结果重放回界面', async () => {
  const d = deferred()
  const { store, stub } = setup(() => d.promise)

  // 先造一份"上一轮"的脏状态：清态必须把它们作废，这条不能被重放路径带回来。
  store.opsResult = { OK: ['a'], Skipped: [], Failed: [], Cancelled: [], Warnings: [], Reclaimed: 1 }
  store.addProcDir('/old-priority')

  store.roots = ['/tmp/whatever']
  store.startScan()
  // 后端抢在 RPC 回包之前发完 done
  stub.emit('scan:done', DONE)
  d.resolve('scan-1')
  await flush()

  assert.equal(store.hasResult, true, '结果集不能被清态永久抹掉（曾表现为"暂无结果"）')
  assert.equal(store.groups.length, 1, '新结果必须回到界面上')
  assert.equal(store.totalGroups, 7)
  assert.equal(Number(store.reclaimableTotal), 12345, '分页回包的全量口径必须覆盖 done 的初始值')
  assert.equal(store.view, 'result', '完成即切结果页')
  assert.equal(store.opsResult, null, '清态本身不能省，否则能对已失效的 fileID 发起操作')
  assert.deepEqual([...store.procDirs], [])
  stub.restore()
})

test('回包早于 done（主路径）：结果照常装载，且不得多做一次分页', async () => {
  const { store, stub } = setup(() => Promise.resolve('scan-1'))
  store.opsResult = { OK: ['a'], Skipped: [], Failed: [], Cancelled: [], Warnings: [], Reclaimed: 1 }
  store.roots = ['/tmp/whatever']
  store.startScan()
  await flush()
  stub.emit('scan:done', DONE)
  await flush()

  assert.equal(store.hasResult, true)
  assert.equal(store.groups.length, 1)
  assert.equal(store.opsResult, null)
  // bindEvents 的初始拉取不含分页；这里只允许 done 处理器那一次取页。
  assert.equal(stub.invoked('GetResultGroups'), 1, '主路径不该出现第二次取页')
  stub.restore()
})

test('第二轮扫描的回包不得重放上一轮的 done 摘要', async () => {
  const first = deferred()
  const second = deferred()
  const queue: unknown[] = [first.promise, second.promise]
  const { store, stub } = setup(() => queue.shift() ?? Promise.resolve('no-scan'))

  // 第一轮：刻意走"回包先、done 后"的主路径（等回包落地再发 done），
  // 才能把"第二轮回包时上一轮摘要已在手上"这个前提干净地造出来。
  store.roots = ['/tmp/whatever']
  store.startScan()
  first.resolve('scan-1')
  await flush()
  assert.equal(stub.invoked('GetResultGroups'), 0, '只回包、还没 done 时不该取页')
  stub.emit('scan:done', DONE)
  await flush()
  assert.equal(store.status, 'Done')
  assert.equal(stub.invoked('GetResultGroups'), 1)

  // 第二轮：只回包，done 还在路上
  store.startScan()
  second.resolve('scan-2')
  await flush()

  assert.equal(store.status, 'Scanning', '第二轮仍在途中，不能被上一轮的摘要判成 Done')
  assert.equal(store.hasResult, false, '第二轮的结果还没到')
  assert.equal(stub.invoked('GetResultGroups'), 1, '不得为陈旧摘要补一次取页')
  stub.restore()
})
