// 功能 3（隐藏非拟处理项）的 store 腿（2026-09-23）。
//
// 钉的是 scan.ts 里 hideNonPending 那条**请求上送链**：开关打开时，每次翻页
// 都要把当前处理策略（白/黑名单）随 GetResultGroups 一起送后端，回包里逐行的
// isPending 才是"按当前策略算的"投影；策略变了要重取本页，否则藏行用的是陈旧
// 策略。判据本体在后端（pendingSetLocked→pendingIDsLocked 同一内核），
// 本文件没有一条用例在前端比路径或猜归属——那正是 AS-H6 禁止的形状。
//
// ★ 为什么"开关关掉"不发请求：关掉后 isPending 不再被读，陈旧值无害；
// 而开着时每一次策略变更都必须有一次重取，否则界面把"会动的"和"藏起来的"
// 摆错位置（藏错行 = 用户以为剩下的都会被处理）。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createPinia, setActivePinia } from 'pinia'

import { installWailsStub, flush } from './harness.mjs'
import { useScanStore } from '../src/stores/scan'

function setup() {
  setActivePinia(createPinia())
  const stub = installWailsStub({
    GetResultGroups: () => ({ groups: [], total: 0, totalReclaimable: 0, totalReclaimableActual: 0 }),
    GetFailedItems: () => [],
    GetStatus: () => 'Idle',
    GetSettings: () => ({ theme: 'light' }),
    GetVersion: () => '0.5.0',
    GetStartupNotice: () => '',
    ListScanHistory: () => [],
  })
  const store = useScanStore()
  store.bindEvents()
  store.hasResult = true // 投影重取的门槛是"有结果集"；本文件测的是请求腿，不是渲染
  return { store, stub }
}

const lastResultQuery = (stub: ReturnType<typeof installWailsStub>) => {
  const calls = stub.calls.filter(c => c.method === 'GetResultGroups')
  return calls[calls.length - 1]?.args[0]
}

test('H1 开关关着：请求不带 dirs/excludeDirs（翻页零预热 I/O）', async () => {
  const t = setup()
  t.store.addProcDir('/vol/a')
  t.store.addProcExcludeDir('/vol/b')
  await t.store.loadResultPage(false)
  const q = lastResultQuery(t.stub)
  assert.equal(q.dirs, undefined, '关着还上送策略目录 ⇒ 每次翻页都触发后端卷语义探测（写盘）')
  assert.equal(q.excludeDirs, undefined)
  t.stub.restore()
})

test('H2 开关打开：立刻重取本页且带上两把过滤器（空白项不进请求）', async () => {
  const t = setup()
  const before = t.stub.invoked('GetResultGroups')
  t.store.addProcDir('/vol/a')
  t.store.addProcDir('  ') // trim 拒绝
  t.store.addProcExcludeDir('/vol/b')
  t.store.toggleHideNonPending()
  await flush()
  assert.equal(t.stub.invoked('GetResultGroups'), before + 1, '打开开关必须重取——旧行的 isPending 是没带策略算的')
  const q = lastResultQuery(t.stub)
  assert.deepEqual(q.dirs, ['/vol/a'])
  assert.deepEqual(q.excludeDirs, ['/vol/b'])
  t.stub.restore()
})

test('H3 开关开着改策略：每次改动都重取，最新请求带最新列表', async () => {
  const t = setup()
  t.store.addProcDir('/vol/a')
  t.store.toggleHideNonPending()
  await flush()
  const n0 = t.stub.invoked('GetResultGroups')

  t.store.addProcDir('/vol/c')
  await flush()
  assert.equal(t.stub.invoked('GetResultGroups'), n0 + 1, '白名单变了却没重取 ⇒ 藏行按陈旧策略投影')
  assert.deepEqual(lastResultQuery(t.stub).dirs, ['/vol/a', '/vol/c'])

  t.store.addProcExcludeDir('/vol/a/sub')
  await flush()
  assert.equal(t.stub.invoked('GetResultGroups'), n0 + 2, '黑名单变了同理')
  assert.deepEqual(lastResultQuery(t.stub).excludeDirs, ['/vol/a/sub'])

  t.store.clearProcDirs()
  await flush()
  assert.deepEqual(lastResultQuery(t.stub).dirs, [], '清空白名单后请求必须反映"该维度未启用"')
  t.stub.restore()
})

test('H4 关掉开关：不重取（陈旧 isPending 不再被读），后续翻页回到无策略形状', async () => {
  const t = setup()
  t.store.addProcDir('/vol/a')
  t.store.toggleHideNonPending()
  await flush()
  t.store.toggleHideNonPending()
  assert.equal(t.store.hideNonPending, false)
  const n = t.stub.invoked('GetResultGroups')
  await flush()
  assert.equal(t.stub.invoked('GetResultGroups'), n, '关掉不需要新数据')
  await t.store.loadResultPage(false)
  assert.equal(lastResultQuery(t.stub).dirs, undefined, '关掉后还带着目录 = 白付预热 I/O')
  t.stub.restore()
})

test('H5 结果集换代（hasResult=false）时策略清空不触发重取', async () => {
  // clearStaleResult 会连带 clearProcDirs/clearProcExcludeDirs；那时结果集已作废，
  // 再发一次 GetResultGroups 会把**后端旧结果**盖回刚清空的界面（新扫描在途）。
  const t = setup()
  t.store.toggleHideNonPending()
  await flush()
  t.store.hasResult = false
  const n = t.stub.invoked('GetResultGroups')
  t.store.clearProcDirs()
  t.store.clearProcExcludeDirs()
  await flush()
  assert.equal(t.stub.invoked('GetResultGroups'), n, '换代清空引发了对旧结果集的重取')
  t.stub.restore()
})
