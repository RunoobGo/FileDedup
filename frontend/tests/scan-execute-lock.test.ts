// §20 第九批 P-20-1（M77，04 §6.11 FE-4；设计段 §20.0-1 / §20.1-1）。
//
// 缺陷原形（改前坐标 scan.ts:716 / :720 / :721）：
//   if (!guard('执行清理操作')) return   ← 读 opsRunning
//   await ensureProcCounts()            ← 启用了处理策略时是一次真 RPC 往返
//   opsRunning.value = true             ← 上锁
// 两次点击因此在同一窗口里都通过 guard。登记只说到"两个入口都能通过 guard"，
// 开码读到的后果更具体（§20.0-1）：第二次会在自己的 catch（:733）里把
// **第一次还在途的**互斥标记解回 false，并顺手改写 opsResult / opsProgress。
//
// 两格断言各自钉一件事（前两条用例）：
//   a) 第二次不得发出后端请求（互斥必须在前端就判死）；
//   b) 第一次在途时，第二次的失败收口不得解开 opsRunning（过早解锁 = 界面重新可点）。
// 第三条用例是本批改动**自己新产生的那一格**（不变式，改前空转）：提前上锁之后，
//   进度条不得留着上一条操作的 Done/Total。就地注释写明了它不是修前红探针。
// ★ 取证更正（写探针时才抓到）：把 b) 单独放进"两次调用中间 await 一次"的用例里是**假绿**——
//   那一次 await 已经让第一次把锁上了，第二次当场被 guard 挡下，b) 在改前也是绿的。
//   b) 必须与 a) 同处一个"两次调用之间没有任何 await"的 tick 才测得到，见下面第一条用例。
//
// 对照：undoOperation(:446) / undoOperationItem(:460) 第一句就上锁，全仓只有 executeOp 有这扇窗。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createPinia, setActivePinia } from 'pinia'

import { installWailsStub, deferred, flush } from './harness.mjs'
import { useScanStore } from '../src/stores/scan'

const file = (id: number, isKeep = false) => ({
  id,
  path: `/vol/a/f${id}.bin`,
  name: `f${id}.bin`,
  size: 2048,
  mtime: 0,
  isKeep,
  volume: 'v1',
  volumeResolved: true,
})

/** 第一次 ExecuteOperation 挂在空中（模拟操作在途），第二次回绝（后端自己的守卫）。 */
function setup(useProcDirs = false) {
  setActivePinia(createPinia())
  const pending = deferred()
  let n = 0
  const counts = deferred()
  const stub = installWailsStub({
    ExecuteOperation: () => {
      n++
      return n === 1 ? pending.promise : Promise.reject(new Error('操作执行中，请等待完成'))
    },
    FilterInDirs: () => counts.promise,
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
  store.groups = [{ groupID: 1, reclaimable: 0, size: 2048, files: [file(101, true), file(102)] }]
  store.toggleSelect(102)
  if (useProcDirs) store.addProcDir('/vol/priority')
  return { store, stub, pending, counts }
}

test('同 tick 连点两次：第二次不得再发 RPC，也不得解开第一次还占着的锁', async () => {
  const { store, stub, pending } = setup()
  void store.executeOp('delete', undefined, true)
  void store.executeOp('delete', undefined, true)
  await flush()
  assert.equal(
    stub.invoked('ExecuteOperation'),
    1,
    'guard 与上锁之间隔着 await ⇒ 两次点击都通过了互斥，第二次被后端回绝前已经占用了一次 RPC',
  )
  assert.equal(
    store.opsRunning,
    true,
    '第二次调用的 catch 解开了第一次操作还占着的互斥标记（第一次的 RPC 至今挂在空中）：' +
      '此后界面在操作在途时重新变成可点，是 P2「操作执行中」死锁的反方向——过早解锁',
  )
  pending.resolve()
  stub.restore()
})

test('在途锁已置、RPC 还没发出时，进度条不得留着上一条操作的数字', async () => {
  // ★ 这条是**不变式**而不是修前红探针：改前此刻 opsRunning 还是 false（横幅根本没出现），
  //   所以断言空转、改前也绿。它钉的是本批把上锁提前之后新产生的那一格——
  //   提前上锁必须同时把 opsProgress 置空，否则进度条会在 await 窗口里显示**上一条**的
  //   Done/Total（那是一句假话）。能被它杀死的变异见设计段 §20.7 的 M20-h。
  const { store, stub, counts } = setup(true)
  store.opsProgress = { Done: 7, Total: 7, Current: '' } // 上一条操作跑满的样子
  void store.executeOp('delete', undefined, true)
  await new Promise((r) => setImmediate(r))
  if (store.opsRunning) {
    assert.equal(
      store.opsProgress,
      null,
      'opsRunning 已为真而 RPC 未发：进度条此刻展示的是上一条操作的 Done/Total',
    )
  } else {
    // 改前形状：锁还没上，横幅根本没出现（记录在案，免得这条用例被读成"两版都绿是因为没测到"）。
    assert.equal(stub.invoked('ExecuteOperation'), 0)
  }
  counts.resolve([0])
  await flush()
  stub.restore()
})

test('启用了处理策略时窗口是一次完整 RPC 往返：第二次同样必须在 guard 处判死', async () => {
  const { store, stub, pending, counts } = setup(true)
  void store.executeOp('delete', undefined, true)
  void store.executeOp('delete', undefined, true)
  counts.resolve([0]) // 命中勾选序列第 0 项 ⇒ 两次调用都会继续往下走
  await flush()
  assert.equal(stub.invoked('ExecuteOperation'), 1, '第二次穿过 guard 发了真请求')
  assert.equal(store.opsRunning, true, '在途锁被注定失败的那一次解开')
  assert.equal(stub.invoked('FilterInDirs'), 1, '命中数问了两次：同一份真值被重复索取')
  pending.resolve()
  stub.restore()
})
