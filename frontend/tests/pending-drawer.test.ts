// 拟处理清单抽屉的状态机（2026-09-23，设计段 §4/§5）。
//
// 钉的是 scan.ts 里 pendingReqSeq 那套"只认最后一次回执"的守卫与三个入口
// （open/翻页/换排序）的请求形状。判据本体（谁进拟处理集）在后端
// pendingIDsLocked，前端不重算——这与 AS-H6 后的 filterInDirs 同一纪律，
// 所以本文件没有一条用例在前端比路径或比保留标记。
//
// 形状与 scan-proc-race 同族：回包停在用例手里，"哪一版先到"由用例决定。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createPinia, setActivePinia } from 'pinia'

import { installWailsStub, deferred, flush } from './harness.mjs'
import { useScanStore } from '../src/stores/scan'

const file = (id: number, isKeep = false) => ({
  id,
  path: `/vol/a/f${id}.bin`,
  name: `f${id}.bin`,
  size: 1024,
  mtime: 0,
  isKeep,
  volume: 'vid:7',
  volumeResolved: true,
})

const pageData = (over: Record<string, unknown> = {}) => ({
  total: 3, page: 0, pageSize: 50,
  pendingCount: 1, keepCount: 1, outsideCount: 1, goneCount: 0,
  rows: [
    { id: 102, path: '/vol/a/f102.bin', name: 'f102.bin', size: 1024, groupId: 7, pending: true, reason: '' },
    { id: 101, path: '/vol/keep/f101.bin', name: 'f101.bin', size: 1024, groupId: 7, pending: false, reason: 'keep' },
    { id: 103, path: '/vol/b/f103.bin', name: 'f103.bin', size: 1024, groupId: 7, pending: false, reason: 'outside' },
  ],
  ...over,
})

function setup() {
  setActivePinia(createPinia())
  const queue: ReturnType<typeof deferred>[] = []
  const stub = installWailsStub({
    GetPendingFiles: () => {
      const d = deferred()
      queue.push(d)
      return d.promise
    },
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
  store.groups = [
    { groupID: 7, reclaimable: 0, size: 3072, files: [file(101, true), file(102), file(103)] },
  ] as any
  store.toggleSelect(102)
  store.toggleSelect(103)
  store.addProcDir('/vol/a')
  // 空白目录进不了 procDirs（addProcDir 已 trim 拒绝），这里再放一枚脏数据
  // 直推数组，钉的是 usableProcDirs 的过滤腿真的在请求链上。
  store.procDirs.push('   ')
  return { store, stub, queue }
}

const lastQuery = (stub: ReturnType<typeof installWailsStub>) => {
  const calls = stub.calls.filter(c => c.method === 'GetPendingFiles')
  return calls[calls.length - 1].args[0]
}

test('P1 请求形状：勾选 ID、去空白后的优先目录、首屏默认分页', async () => {
  const t = setup()
  t.store.openPendingDrawer()
  const q = lastQuery(t.stub)
  assert.deepEqual(q.selectedIds, [102, 103], '勾选集原样上送（顺序=勾选遍历序）')
  assert.deepEqual(q.dirs, ['/vol/a'], '空白目录不得发出（与 executeOp 的 usableProcDirs 同口径）')
  assert.equal(q.page, 0)
  assert.equal(q.pageSize, 50)
  assert.equal(q.sort, 'group')

  t.queue[0].resolve(pageData())
  await flush()
  assert.equal(t.store.pendingData?.total, 3)
  assert.equal(t.store.pendingLoading, false)
  t.stub.restore()
})

test('P2 回包错乱：旧页的回执后到，不得覆盖新页状态、不得把 loading 按灭又点亮', async () => {
  const t = setup()
  t.store.openPendingDrawer()                       // req0：page 0
  t.store.pendingGotoPage(1)                        // req1：page 1
  assert.equal(t.stub.invoked('GetPendingFiles'), 2)

  t.queue[1].resolve(pageData({ page: 1, total: 120 }))
  await flush()
  assert.equal(t.store.pendingData?.page, 1, '新回执先到就该立刻是第 1 页')

  t.queue[0].resolve(pageData({ page: 0 })) // 陈旧回包后到
  await flush()
  assert.equal(t.store.pendingData?.page, 1, '旧回执把清单换回了第 0 页')
  assert.equal(t.store.pendingLoading, false, '陈旧回执不得再动在途标志')
  t.stub.restore()
})

test('P3 换排序回第 0 页：停在旧页会读到一条没排过的尾巴', async () => {
  const t = setup()
  t.store.openPendingDrawer()
  t.queue[0].resolve(pageData({ total: 120 }))
  await flush()
  t.store.pendingGotoPage(2)
  t.queue[1].resolve(pageData({ total: 120, page: 2 }))
  await flush()
  assert.equal(t.store.pendingPage, 2)

  t.store.pendingSetSort('size')
  const q = lastQuery(t.stub)
  assert.equal(q.sort, 'size')
  assert.equal(q.page, 0, '换排序必须重拉第 0 页')
  assert.equal(t.store.pendingTotalPages, 3, '120 项 / 50 一页 = 3 页')
  t.queue[2].resolve(pageData({ total: 120, page: 0 }))
  await flush()
  assert.equal(t.store.pendingData?.page, 0)
  t.stub.restore()
})

test('P4 关闭作废在途回执：慢回包不得塞进"下一次打开"的第一帧', async () => {
  const t = setup()
  t.store.openPendingDrawer()
  t.store.closePendingDrawer()

  t.queue[0].resolve(pageData()) // 关闭后旧回执才到
  await flush()
  assert.equal(t.store.pendingData, null, '作废的回执仍把上一次快照写进了状态')
  assert.equal(t.store.pendingLoading, false)

  t.store.openPendingDrawer()    // 下一次打开重新问
  assert.equal(t.stub.invoked('GetPendingFiles'), 2)
  t.queue[1].resolve(pageData({ pendingCount: 2 }))
  await flush()
  assert.equal(t.store.pendingData?.pendingCount, 2)
  t.stub.restore()
})

test('P5 拉取失败说清楚、重拉成功后错误必须收掉', async () => {
  const t = setup()
  t.store.openPendingDrawer()
  t.queue[0].reject(new Error('后端不可用'))
  await flush()
  assert.match(t.store.pendingError, /后端不可用/)
  assert.equal(t.store.pendingData, null, '失败时不得保留上一次的清单还装作是新的')

  t.store.pendingGotoPage(1)
  t.queue[1].resolve(pageData({ page: 1 }))
  await flush()
  assert.equal(t.store.pendingError, '', '成功回包后陈旧错误行还挂着 = 界面对同一件事说两句相反的话')
  assert.equal(t.store.pendingData?.page, 1)
  t.stub.restore()
})
