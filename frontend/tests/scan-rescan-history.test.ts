// M78（2026-09-21 审查）：rescanHistory 不得再靠"逐字段手抄"回填过滤器。
//
// 缺陷原形：emptyFilters() 有 7 位（scan.ts:14），rescanHistory 只抄了 6 位，
// 漏的正是 M6-P1 的 AllowCloudHydration。于是"用历史配置重扫"这一条路径会把
// 用户当初显式打开的云端占位档位**静默降级**成安全档，而界面显示的仍是同一份历史
// 配置——配置口径与实跑口径分叉，且没有任何提示。
//
// 这条用例同时钉住修法为什么是"整体透传"而不是"补一行"：补一行只治这一次漏抄。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createPinia, setActivePinia } from 'pinia'

import { installWailsStub, flush } from './harness.mjs'
import { useScanStore } from '../src/stores/scan'
import type { HistoryMeta } from '../src/wails'

function setup() {
  setActivePinia(createPinia())
  const stub = installWailsStub({
    GetResultGroups: () => ({ groups: [], total: 0, totalReclaimable: 0 }),
    GetFailedItems: () => [],
    GetStatus: () => 'Idle',
    GetSettings: () => ({ theme: 'light' }),
    GetVersion: () => '0.5.0',
    GetStartupNotice: () => '',
    StartScan: () => ({}),
  })
  const store = useScanStore()
  return { store, stub }
}

/** 取本轮发给后端的 StartScan 载荷。 */
function payload(stub: any) {
  const call = stub.calls.find((c: any) => c.method === 'StartScan')
  assert.ok(call, 'rescanHistory 没有发出 StartScan')
  return call.args[0]
}

const meta = (filters: unknown): HistoryMeta =>
  ({
    id: 1, savedAt: 1, roots: ['/vol'], groups: 0, files: 0, origFiles: 0,
    reclaimable: 0, threads: 2, paranoid: false, filters,
  }) as unknown as HistoryMeta

test('历史里的 AllowCloudHydration 必须透传到重扫载荷（M78 主案）', async () => {
  const { store, stub } = setup()
  store.rescanHistory(meta({
    IncludeExts: [], ExcludeExts: [], MinSize: 0, MaxSize: 0,
    ExcludePaths: [], IncludeHidden: false, AllowCloudHydration: true,
  }))
  await flush()
  assert.equal(payload(stub).Filters.AllowCloudHydration, true,
    '重扫把云端占位档位静默降级成了安全档（改前形状）')
  stub.restore()
})

test('将来 Filters 新增字段时自动透传（钉住"整体透传"这个修法本身）', async () => {
  const { store, stub } = setup()
  // 用一个今天不存在于 Filters 类型里的键模拟"下一次加字段"：
  // 只要历史里有值，重扫就必须带着它，不再依赖有人记得去抄。
  store.rescanHistory(meta({ AllowCloudHydration: false, SomeFutureSwitch: 'keep' }))
  await flush()
  assert.equal((payload(stub).Filters as any).SomeFutureSwitch, 'keep',
    '新字段又被漏抄了——逐字段列举的必然复现路径')
  stub.restore()
})

test('Go 侧空切片回包的 null 不得写进 store（数组口径要兜住）', async () => {
  const { store, stub } = setup()
  // model.Filters 无 omitempty：nil 切片 marshal 成 JSON null。改前的 `?? []` 挡住了，
  // 整体透传若不做归一就会把 IncludeExts 写成 null，下游 .join() 直接炸。
  store.rescanHistory(meta({
    IncludeExts: null, ExcludeExts: null, ExcludePaths: null,
    MinSize: null, MaxSize: null, IncludeHidden: null, AllowCloudHydration: null,
  }))
  await flush()
  assert.deepEqual(store.filters.IncludeExts, [], 'IncludeExts 被 null 污染')
  assert.deepEqual(store.filters.ExcludeExts, [], 'ExcludeExts 被 null 污染')
  assert.deepEqual(store.filters.ExcludePaths, [], 'ExcludePaths 被 null 污染')
  assert.equal(store.filters.MinSize, 0, 'MinSize 被 null 污染')
  // null 一律回落安全档：AllowCloudHydration 不得因为 null 变成 truthy
  assert.equal(store.filters.AllowCloudHydration, false)
  stub.restore()
})

test('字节口径只换算一次（重扫载荷仍回字节，store 保持 KB）', async () => {
  const { store, stub } = setup()
  store.rescanHistory(meta({ MinSize: 2048, MaxSize: 10240 }))
  await flush()
  assert.equal(store.filters.MinSize, 2, 'store 侧应回填 KB 口径')
  assert.equal(store.filters.MaxSize, 10)
  assert.equal(payload(stub).Filters.MinSize, 2048, '出口应换回字节，且不得二次换算')
  assert.equal(payload(stub).Filters.MaxSize, 10240)
  stub.restore()
})
