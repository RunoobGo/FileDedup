// R3-2（2026-09-23 第四轮全仓审查，设计段 §30.8）：确认框那个"将处理 N / 已排除 M"
// 的**唯一数字来源**（`scan.ts` 的 procReqSeq 陈旧回包守卫）此前零覆盖。
//
// 形状与既有 race 用例同族：`startScan` 的 `resultGen`、历史横幅的 `histGen` 各有一枚
// race 钉（scan-race / scan-history-race），只有 `refreshProcCounts` 这条没有。
// 判据：`const seq = ++procReqSeq` + 两条回执腿各一次 `if (seq !== procReqSeq) return`。
// 删掉守卫会怎样：用户在 150ms 去抖窗口里改了勾选，**旧**回包后到时把
// `procMatch` 整个换成按旧勾选序列算出的下标 ⇒ 确认框在一份已经不是当前的选中集上
// 报"将处理 N"。这不是显示层小错，N 是用户点下确认按钮的依据。
//
// ★ 两条"到达顺序"各一条用例（O1 顺序 / O2 逆序），终态必须相同：
//   只写逆序那一条会被读成"守卫只在一种时序下承重"，只写顺序那一条删掉守卫照样绿。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createPinia, setActivePinia } from 'pinia'

import { installWailsStub, deferred, flush } from './harness.mjs'
import { useScanStore } from '../src/stores/scan'
import { useToastStore } from '../src/stores/toast'

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

/**
 * 起一个真 store，`FilterInDirs` 的回包全部停在调用方手里（每次请求一枚 deferred）。
 * 这样"哪一版先到"由用例说了算，而不是由微任务顺序碰运气。
 */
function setup() {
  setActivePinia(createPinia())
  const queue: ReturnType<typeof deferred>[] = []
  const stub = installWailsStub({
    FilterInDirs: () => {
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
    { groupID: 1, reclaimable: 0, size: 3072, files: [file(101, true), file(102), file(103)] },
  ]
  store.addProcDir('/vol/a')
  return { store, stub, queue, toast: useToastStore() }
}

// 两次请求：第一次只勾 102（回包 [0] = 1 项命中），中途再勾上 103
// （第二次的勾选序列是 [102,103]，回包 [0,1] = 2 项命中）。
// ⇒ 终态只可能是 2；任何时刻读回 1 都说明旧回包落在了新状态上。
function twoRequests(t: { store: ReturnType<typeof useScanStore> }) {
  const { store } = t
  store.toggleSelect(102)
  const first = store.ensureProcCounts()
  store.toggleSelect(103)
  const second = store.ensureProcCounts()
  return { first, second }
}

test('O1 顺序（旧先回、新后回）：终态是新勾选序列的那一份', async () => {
  const t = setup()
  const { first, second } = twoRequests(t)
  assert.equal(t.stub.invoked('FilterInDirs'), 2, '两次勾选变化各问了一次')

  t.queue[0].resolve([0])
  await flush()
  // ★ 期望按现实现取真值（不是"旧回包先落地就先显示旧数"）：守卫比的是**请求序号**而不是
  //   回包新旧，seq=1 遇上 procReqSeq=2 ⇒ 旧那一问整个丢掉，连"暂时显示 1"都不该发生。
  assert.equal(t.store.effectiveCount, 0, '旧回包被写进了状态（此刻当前序列的真值还没到）')
  assert.equal(t.store.procCountPending, true, '新序列的真值还没到 ⇒ 必须仍说"计算中"，不许借旧数顶上')

  t.queue[1].resolve([0, 1])
  await Promise.all([first, second])
  assert.equal(t.store.effectiveCount, 2)
  assert.equal(t.store.procCountPending, false)
  assert.equal(t.store.procCountError, '')
  t.stub.restore()
})

test('O2 逆序（新先回、旧的后到）：旧回包不得覆盖已到位的新真值', async () => {
  const t = setup()
  const { first, second } = twoRequests(t)

  t.queue[1].resolve([0, 1])
  await flush()
  assert.equal(t.store.effectiveCount, 2, '新回包先到就该立刻是真值')
  assert.equal(t.store.procCountPending, false)

  t.queue[0].resolve([0]) // 陈旧回包后到
  await Promise.all([first, second])
  await flush()
  assert.equal(
    t.store.effectiveCount,
    2,
    '旧回包把 procMatch 换成了按**旧勾选序列**算的下标 ⇒ 确认框会在已经不是当前的选中上报数',
  )
  assert.equal(t.store.procCountPending, false, '被旧回包顶掉后会永久"计算中"（key 与当前指纹不等）')
  assert.equal(t.store.procCountError, '')
  t.stub.restore()
})

test('O3 陈旧回包**失败**：不得把已到位的真值打回错误态，也不得多弹一条提示', async () => {
  const t = setup()
  const { first, second } = twoRequests(t)

  t.queue[1].resolve([0, 1])
  await flush()
  assert.equal(t.store.effectiveCount, 2)

  t.queue[0].reject(new Error('上一问超时')) // 旧那一问失败
  await Promise.all([first, second.catch(() => {})])
  await flush()
  assert.equal(t.store.procCountError, '', '陈旧请求的失败写进了当前状态')
  assert.equal(t.store.effectiveCount, 2, '已经到位的真值被一次注定作废的失败抹掉')
  assert.equal(t.toast.toasts.length, 0, `多弹了 toast：${JSON.stringify(t.toast.toasts.map(x => x.msg))}`)
  assert.equal(t.stub.invoked('FilterInDirs'), 2, '失败引发了重问（第三次 RPC）')
  t.stub.restore()
})

test('O4 同一份"优先目录 + 勾选"连问两次：只发一次 RPC', async () => {
  const t = setup()
  t.store.toggleSelect(102)
  const a = t.store.ensureProcCounts()
  t.queue[0].resolve([0])
  await a
  const b = t.store.ensureProcCounts()
  await b
  assert.equal(t.stub.invoked('FilterInDirs'), 1, 'procMatch.key === 当前指纹那条短路被改走了')
  assert.equal(t.store.effectiveCount, 1)
  t.stub.restore()
})

test('O5 对照（防 O3 空转）：**当前**那一问失败必须看得见', async () => {
  // 这条不是修前红探针，是 O3 的负控制：如果"失败一律不写状态"，O3 与真缺陷同形。
  const t = setup()
  t.store.toggleSelect(102)
  const p = t.store.ensureProcCounts()
  t.queue[0].reject(new Error('后端 FilterInDirs 崩了'))
  await p
  assert.match(t.store.procCountError, /FilterInDirs 崩了/)
  assert.equal(t.store.effectiveCount, 0, '真值没到位时不得凭猜测给数')
  assert.equal(t.store.procCountPending, true)
  assert.equal(t.toast.toasts.length, 1, '当前失败必须提示一次')
  t.stub.restore()
})
