// M386（2026-10-01 第九轮 P1-5）：翻「隐藏非拟处理项」这一类**显示偏好重取**不得把
// 已加载的行集塌回一页，也不得因此丢掉第 100 行之后的勾选。
//
// 改前的形状（现读 scan.ts 的非 append 分支）：请求把 `pageSize` 写死成 100 ⇒
// groups 被换成第 0 页的 100 行、resultPage 回到 1，于是
//   · 用户经 loadMore 累积到 800 组，翻一次开关就只剩 100 组（没人请求这件事）；
//   · 第 100 组之后的勾选项在新行集里"不可见" ⇒ replaySelection 按 M342 的判据把它们丢掉。
//     这正是 09 §6.1 / 10 §2.4 承诺"纯显示层、勾选一字不变"的残半。
//
// ★ 为什么这里既有纯函数格又有 store 格：设计段原本以为"pinia store 打不进 node --test"，
//   只想做静态接线锚。那句不成立——tests/harness.mjs（M16）就是拿一枚 Proxy 桩在 node 里
//   实例化**真 store** 的（活例：scan-resultset.test.ts）。所以 P-58 落成行为探针：
//   真的累积行集、真的翻开关、真的读 groups 与 selection。措辞上仍是"探针"而不是"端到端"：
//   后端是桩，段与段之间的真实并发换代（视图重排）在本通道里读不到，已如实记进设计段 §五 D1 残余风险。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createPinia, setActivePinia } from 'pinia'

import { projectionReload, MAX_PAGE_SIZE } from '../src/utils/pageload'
import { installWailsStub, flush } from './harness.mjs'
import { useScanStore } from '../src/stores/scan'

// ---------- P-57：分段计划的纯函数判据 ----------

test('P-57 已加载 800 行：分两段取回（每段不超后端上限）+ 页号回到已加载段之后', () => {
  assert.deepEqual(projectionReload(800, 100), { segmentSize: 500, segments: 2, nextPage: 8 })
})

test('P-57 放量到 2000 行（DEFAULT_LOAD_CAP 初始档）：四段、页号 20', () => {
  assert.deepEqual(projectionReload(2000, 100), { segmentSize: 500, segments: 4, nextPage: 20 })
})

test('P-57 负控制：不足一页时与改前同形（单段、页号 1、段大小就是步长）', () => {
  assert.deepEqual(projectionReload(50, 100), { segmentSize: 100, segments: 1, nextPage: 1 })
  assert.deepEqual(projectionReload(100, 100), { segmentSize: 100, segments: 1, nextPage: 1 })
  assert.deepEqual(projectionReload(0, 100), { segmentSize: 100, segments: 1, nextPage: 1 })
})

test('P-57 恰好等于后端上限：一段就够，但页号必须跟着行数走', () => {
  assert.deepEqual(projectionReload(500, 100), { segmentSize: 500, segments: 1, nextPage: 5 })
})

test('P-57 算术不变量①：分段容量取得回已加载的那一段（segments×segmentSize ≥ loaded）', () => {
  for (const loaded of [101, 199, 500, 501, 800, 1234, 2000, 5000]) {
    const p = projectionReload(loaded, 100)
    assert.ok(
      p.segments * p.segmentSize >= loaded,
      `loaded=${loaded}：分段容量 ${p.segments * p.segmentSize} 取不回已加载行`,
    )
    assert.ok(p.segmentSize <= MAX_PAGE_SIZE, `loaded=${loaded}：段大小 ${p.segmentSize} 会撞后端钳位`)
    assert.ok(p.segmentSize >= 100, `loaded=${loaded}：段大小小于步长会把偏移算到行集中间`)
  }
})

test('P-57 算术不变量②：页号既不能重复取回、也不能跳过未看过的行', () => {
  // ★ 这一格就是设计段原方案（"一次发 size=loaded、页号照 loaded/100 写"）漏掉的那格：
  //   后端把 pageSize 钳在 500，800 行那一档实际只回 500 行，却把 resultPage 写成 8 ⇒
  //   下一次追加从偏移 800 起，第 500~799 组被**永久跳过**。修法本身比原缺陷更会丢数据，
  //   所以这条不变量必须独立成格（它守的是"页号×步长落在已加载段的边界上"）。
  for (const loaded of [101, 199, 500, 501, 800, 1234, 2000, 5000]) {
    const p = projectionReload(loaded, 100)
    assert.ok(p.nextPage * 100 >= loaded, `loaded=${loaded}：页号 ${p.nextPage} 偏小，追加会重复取回已加载行`)
    assert.ok((p.nextPage - 1) * 100 < loaded, `loaded=${loaded}：页号 ${p.nextPage} 偏大，追加会跳过未看过的行`)
    assert.ok(p.segments * p.segmentSize < loaded + p.segmentSize,
      `loaded=${loaded}：分段数偏多，会多发一次没人要的请求`)
  }
})

// ---------- P-58：store 级行为探针 ----------

/** 第 i 组的两个文件 id：可勾的是 i*10+1，保留项（isKeep）是 i*10+2。 */
function universeGroup(i: number) {
  return {
    files: [
      { id: i * 10 + 1, isKeep: false, path: `/vol/a/f${i}` },
      { id: i * 10 + 2, isKeep: true, path: `/vol/a/k${i}` },
    ],
  }
}

/**
 * 真 store + 会分页的桩后端。
 *
 * `state.size` 可控：把结果集在两轮之间改小，就是在模拟"策略一生效，过滤后的组数变少了"
 * （后端提前到底那一格靠它，不靠改生产代码）。
 */
function setupStore(state = { size: 1000 }) {
  setActivePinia(createPinia())
  const queries: any[] = []
  const stub = installWailsStub({
    GetResultGroups: (q: any) => {
      queries.push(q)
      const start = q.page * q.pageSize
      const rows: any[] = []
      for (let i = start; i < Math.min(start + q.pageSize, state.size); i++) rows.push(universeGroup(i))
      return {
        groups: rows,
        total: state.size,
        page: q.page,
        totalReclaimable: state.size,
        totalReclaimableActual: state.size,
      }
    },
    GetFailedItems: () => [],
    GetStatus: () => 'Idle',
    GetSettings: () => ({ theme: 'light' }),
    GetVersion: () => '0.1.0',
    GetStartupNotice: () => '',
  })
  const store = useScanStore()
  store.bindEvents()
  store.hasResult = true
  const cleanup = () => {
    store.hideNonPending = false
    store.procDirs = []
    stub.restore()
  }
  return { store, stub, queries, state, cleanup }
}

/** 首屏一页 + 追加 extraPages 次 ⇒ 行集累积到 (extraPages+1)×100 组。 */
async function accumulate(store: any, extraPages: number) {
  await store.loadResultPage(false)
  for (let i = 0; i < extraPages; i++) await store.loadMore()
}

test('P-58 主格：累积 800 行后翻显示偏好开关 ⇒ 行集不塌、第 100 行后的勾选仍在（改前必红）', async () => {
  const { store, queries, cleanup } = setupStore()
  try {
    await accumulate(store, 7)
    assert.equal(store.groups.length, 800, '夹具自证：追加腿确实累积到了 800 组')
    assert.equal(store.resultPage, 8, '夹具自证：追加腿记到第 8 页')

    const lateId = 1501 // 第 150 组的可勾文件（id = 组号×10+1）⇒ 落在第 100 行之后，
    // 改前那 100 行以外的组根本不在行集里，replaySelection 会把这个勾判成"不可见"而丢掉。
    store.selection.add(lateId)
    store.procDirs = ['/vol/a']
    store.toggleHideNonPending() // 走 refreshPendingProjection → loadResultPage(false, true)
    await flush()

    assert.equal(store.groups.length, 800, '显示偏好重取把行集塌回了一页（M386 的改前形状）')
    assert.ok(store.selection.has(lateId), '第 100 行之后的勾选被静默丢掉（M342 承诺的残半）')
    assert.equal(store.resultPage, 8, '重取后页号要留在已加载段之后，否则追加会重复取回')

    const reload = queries.slice(-2)
    assert.deepEqual(reload.map((q) => q.pageSize), [MAX_PAGE_SIZE, MAX_PAGE_SIZE], '分段大小不得超后端钳位')
    assert.deepEqual(reload.map((q) => q.page), [0, 1], '分段页号必须连续（跳段就是漏行）')
    assert.ok(reload.every((q) => Array.isArray(q.dirs)), '重取必须带当前策略目录，否则算的是旧口径投影')
  } finally {
    cleanup()
  }
})

test('P-58 不跳行：重取之后的追加腿接着取第 800 组之后的行（而不是从更远处开始）', async () => {
  const { store, cleanup } = setupStore()
  try {
    await accumulate(store, 7)
    store.procDirs = ['/vol/a']
    store.hideNonPending = true
    await store.loadResultPage(false, true)
    assert.equal(store.groups.length, 800)

    await store.loadMore() // page 8 / pageSize 100 ⇒ 偏移 800，正是已加载段之后
    assert.equal(store.groups.length, 900, '重取与追加之间的偏移接不上 ⇒ 中间的行被跳过了')
    assert.equal(store.groups[800].files[0].id, 8001, '第 801 组应是全局第 801 组（无缝接续）')
  } finally {
    cleanup()
  }
})

test('P-58 后端提前到底：某一段回包不满就停，不再多发没人要的段', async () => {
  const state = { size: 2000 }
  const { store, queries, cleanup } = setupStore(state)
  try {
    await accumulate(store, 19) // 累积到放量上限 2000 行
    assert.equal(store.groups.length, 2000)
    assert.equal(store.resultPage, 20)

    state.size = 600 // 策略一生效，过滤后的结果集只剩 600 组（后端到底变早）
    const before = queries.length
    store.procDirs = ['/vol/a']
    store.hideNonPending = true
    await store.loadResultPage(false, true)

    assert.equal(queries.length - before, 2, '第 2 段已回包不满 ⇒ 第 3、4 段不该再发')
    assert.equal(store.groups.length, 600, '行集应等于后端实际还能给出的那一段，不多不少')
    assert.equal(store.resultPage, 20, '页号按计划写回；结果集已到底，追加腿自然拿不到更多行')

    await store.loadMore()
    assert.equal(store.groups.length, 600, '到底之后追加不得凭空造行')
  } finally {
    cleanup()
  }
})

test('P-58b 负控制：排序/筛选那一类重取（keepSelection=false）一字不变', async () => {
  const { store, queries, cleanup } = setupStore()
  try {
    await accumulate(store, 7)
    const before = queries.length
    await store.loadResultPage(false) // 不带 keepSelection：这是"视图换了"，本来就该回到第一页
    assert.equal(queries.length - before, 1, '普通重取不得被分段泄漏成多次请求')
    assert.equal(queries[queries.length - 1].pageSize, 100, '普通重取的 pageSize 必须仍是 store 的步长')
    assert.equal(store.groups.length, 100, '普通重取回到第一页是既有契约（Y8），本批不改它')
    assert.equal(store.resultPage, 1)
  } finally {
    cleanup()
  }
})

test('P-58b 负控制：追加腿（append）不受本批改法影响，仍用步长 100 与自增页号', async () => {
  const { store, queries, cleanup } = setupStore()
  try {
    await store.loadResultPage(false)
    await store.loadMore()
    const q = queries[queries.length - 1]
    assert.equal(q.pageSize, 100)
    assert.equal(q.page, 1)
    assert.equal(store.groups.length, 200)
  } finally {
    cleanup()
  }
})
