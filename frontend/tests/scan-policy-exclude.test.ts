// 功能 2（不处理目录黑名单）的前端合并腿（2026-09-23）。
//
// 判据本体在后端 pendingIDsLocked（Go 侧由 app_policy_exclude_test.go 钉死），
// 前端只做一件事：把「白名单命中」与「黑名单命中」两次 FilterInDirs 的回包
// 按下标做集合减法（scan.ts refreshProcCounts）。这套用例钉的就是这条减法腿：
//   - 只启用黑名单时必须**恰好问一次**后端（多问 = 白名单腿漏了短路，抓包里
//     会出现一个 dirs=[] 的假请求）；
//   - 两腿都在时必须**各问一次且先白后黑**，合并结果 = in − ex；
//   - 黑名单变化必须让陈旧真值立刻失效（procSelKey 少了黑名单一项的话，
//     界面会继续显示改黑名单**之前**的"将处理 N"——那是 AS-H6 双判据事故的
//     前端复发形式）。
// 路径判定一律不上前端（fakeFilter 只是桩的回包生成器，断言里没有任何
// 前端比路径的写法——那正是被测代码不该做的事）。
//
// 竞态（乱序回包、陈旧回包覆盖）已由 scan-proc-race.test.ts 按同一 seq 守卫
// 钉过，本文件不复刻；那里只勾白名单的旧用例同时是"未启用黑名单时零额外 RPC"
// 的回归对照。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createPinia, setActivePinia } from 'pinia'

import { installWailsStub, flush } from './harness.mjs'
import { useScanStore } from '../src/stores/scan'

const file = (id: number, path: string, isKeep = false) => ({
  id,
  path,
  name: path.split('/').pop() ?? path,
  size: 1024,
  mtime: 0,
  isKeep,
  volume: 'vid:7',
  volumeResolved: true,
})

/** 桩版后端判据：返回 paths 中落在 dirs 任一目录（含子树）里的下标升序。
 *  与真实 FilterInDirs 同形状（入参下标集），只够驱动合并逻辑，不参与断言。 */
function fakeFilter(dirs: string[], paths: string[]): number[] {
  const hit: number[] = []
  paths.forEach((p, i) => {
    if (dirs.some(d => p === d || p.startsWith(d.endsWith('/') ? d : `${d}/`))) hit.push(i)
  })
  return hit
}

function setup() {
  setActivePinia(createPinia())
  const stub = installWailsStub({
    FilterInDirs: fakeFilter,
    GetPendingFiles: () => ({
      total: 0, page: 0, pageSize: 50,
      pendingCount: 0, keepCount: 0, outsideCount: 0, goneCount: 0, excludedCount: 0,
      rows: [],
    }),
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
  // 4 项一组：101 保留锚，勾选集固定为 102(/vol/a)、103(/vol/b)、104(/vol/a/sub)
  // ⇒ 下标 0/1/2 分别对应它们，白名单 /vol/a 命中 [0,2]，黑名单 /vol/a/sub 命中 [2]。
  store.groups = [
    {
      groupID: 7, reclaimable: 0, size: 4096,
      files: [
        file(101, '/vol/a/f101.bin', true),
        file(102, '/vol/a/f102.bin'),
        file(103, '/vol/b/f103.bin'),
        file(104, '/vol/a/sub/f104.bin'),
      ],
    },
  ]
  store.toggleSelect(102)
  store.toggleSelect(103)
  store.toggleSelect(104)
  return { store, stub }
}

const filterCalls = (stub: ReturnType<typeof installWailsStub>) =>
  stub.calls.filter(c => c.method === 'FilterInDirs')

test('E1 只启用黑名单：恰好一次 RPC，命中数 = 勾选数 − 黑名单命中数', async () => {
  const t = setup()
  t.store.addProcExcludeDir('/vol/b')
  const p = t.store.ensureProcCounts()
  assert.equal(t.stub.invoked('FilterInDirs'), 1, '白名单没启用却多问了一次（Promise.resolve 短路腿被改走）')
  assert.deepEqual(filterCalls(t.stub)[0].args, [['/vol/b'], ['/vol/a/f102.bin', '/vol/b/f103.bin', '/vol/a/sub/f104.bin']])
  assert.equal(t.store.procCountPending, true, '真值未到位时按钮必须处于等待态')
  await p
  assert.equal(t.store.effectiveCount, 2, '103 在黑名单内，不该被算进"将处理"')
  assert.equal(t.store.effectiveFiles.map(f => f.id).join(','), '102,104')
  assert.equal(t.store.procExcluded, 1)
  assert.equal(t.store.procCountError, '')
  t.stub.restore()
})

test('E2 白名单+黑名单同时启用：两腿各问一次（先白后黑），合并 = in − ex', async () => {
  const t = setup()
  t.store.addProcDir('/vol/a')       // 命中 [0,2]
  t.store.addProcExcludeDir('/vol/a/sub') // 命中 [2]
  await t.store.ensureProcCounts()
  const calls = filterCalls(t.stub)
  assert.equal(calls.length, 2, '合并判据必须两腿都问到后端')
  assert.deepEqual(calls[0].args[0], ['/vol/a'], '第一腿应是白名单（顺序反了抓包会误读，且陈旧黑名单腿会抢 seq）')
  assert.deepEqual(calls[1].args[0], ['/vol/a/sub'])
  assert.equal(t.store.effectiveCount, 1, '104 同时命中两把过滤器——黑名单在后，必须落空')
  assert.equal(t.store.effectiveFiles[0].id, 102)
  assert.equal(t.store.procExcluded, 2, '103（白名单没放行）+ 104（黑名单拦下）都算落空')
  t.stub.restore()
})

test('E3 黑名单变化即刻作废陈旧真值；空白/重复项不进请求', async () => {
  const t = setup()
  t.store.addProcDir('/vol/a')
  await t.store.ensureProcCounts()
  assert.equal(t.store.effectiveCount, 2)
  assert.equal(t.store.procCountPending, false)

  t.store.addProcExcludeDir('   ') // addProcExcludeDir trim 后拒绝，key 不该动
  assert.equal(t.store.procCountPending, false, '空白项改动了指纹（会引发一次无意义的重问）')

  t.store.addProcExcludeDir('/vol/a')
  assert.equal(t.store.procCountPending, true, '黑名单加了，界面还在显示改之前的"将处理 2"')
  await t.store.ensureProcCounts()
  assert.equal(t.store.effectiveCount, 0, '全部勾选项都在黑名单内')
  assert.equal(t.store.procExcluded, 3)

  t.store.addProcExcludeDir('/vol/a') // 去重追加
  await flush()
  assert.equal(t.stub.invoked('FilterInDirs'), 3, '重复项引发了第四次（上限 = 单白 1 + 黑白两腿 2）')
  assert.deepEqual(t.store.procExcludeDirs, ['/vol/a'])
  t.stub.restore()
})

test('E4 executeOp 上送载荷：黑名单非空才传 ExcludeDirs，空则整个字段缺席', async () => {
  const t = setup()
  t.store.addProcDir('/vol/a')
  t.store.addProcExcludeDir('/vol/b')
  await t.store.executeOp('trash')
  const op = t.stub.calls.find(c => c.method === 'ExecuteOperation')
  assert.ok(op, 'ExecuteOperation 没被调用')
  assert.deepEqual(op!.args[0], {
    Kind: 'trash', FileIDs: [102, 103, 104], TargetDir: '', ConfirmDanger: false,
    ProcessDirs: ['/vol/a'], ExcludeDirs: ['/vol/b'],
  }, '载荷必须把两把过滤器原样交给后端——判据在后端，前端只许转述')

  const t2 = setup()
  await t2.store.executeOp('trash')
  const op2 = t2.stub.calls.find(c => c.method === 'ExecuteOperation')!.args[0]
  assert.equal(op2.ExcludeDirs, undefined, '没启用黑名单时传空数组——"没用过这把过滤器"要在抓包里可见')
  assert.equal(op2.ProcessDirs, undefined)
  assert.equal(t2.stub.invoked('FilterInDirs'), 0)

  // 空白黑名单不进载荷（与 usableProcExcludeDirs 同口径）
  const t3 = setup()
  t3.store.addProcExcludeDir('/vol/b')
  t3.store.removeProcExcludeDir(0)
  t3.store.addProcExcludeDir(' ') // trim 拒绝，数组保持空
  await t3.store.executeOp('trash')
  assert.equal(
    t3.stub.calls.find(c => c.method === 'ExecuteOperation')!.args[0].ExcludeDirs,
    undefined,
  )
  t.stub.restore(); t2.stub.restore(); t3.stub.restore()
})

test('E5 拟处理清单请求带 excludeDirs（清单与计数同一判据，不许各问各的）', async () => {
  const t = setup()
  t.store.addProcDir('/vol/a')
  t.store.addProcExcludeDir('/vol/b')
  t.store.openPendingDrawer()
  const q = t.stub.calls.filter(c => c.method === 'GetPendingFiles').at(-1)!.args[0]
  assert.deepEqual(q.dirs, ['/vol/a'])
  assert.deepEqual(q.excludeDirs, ['/vol/b'], '清单没带黑名单 ⇒ 抽屉会列出后端其实不会动的项')
  assert.deepEqual(q.selectedIds, [102, 103, 104])
  await flush()
  t.store.closePendingDrawer()
  t.stub.restore()
})
