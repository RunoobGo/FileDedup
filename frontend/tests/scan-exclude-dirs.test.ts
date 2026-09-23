// 功能1（2026-09-23）：扫描排除目录 Filters.ExcludeDirs 的 store 侧契约。
//
// 三条各钉一个方向：
//   1) 默认值是空数组（安全侧＝不排除），且必须是**数组**而不是 undefined——
//      ScanView 直接对它 .push() / .includes()，缺位即首点即炸。
//   2) startScan 唯一出口整体透传（startScan 的 {...filters} 通道）。
//   3) 旧历史行没有该键 / Go 空切片回包成 null —— 重扫回填后仍是 []，
//      不得把 null 写进 store（rescanHistory 的 null 过滤器是泛型通道，
//      这里钉的是它对 ExcludeDirs 同样成立）。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createPinia, setActivePinia } from 'pinia'

import { installWailsStub, flush } from './harness.mjs'
import { useScanStore, emptyFilters } from '../src/stores/scan'
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

function payload(stub: any) {
  // 取**最近一次** StartScan：同一 stub 下可能发起多轮扫描（本文件第三条用例连发三轮）
  const calls = stub.calls.filter((c: any) => c.method === 'StartScan')
  assert.ok(calls.length > 0, '没有发出 StartScan')
  return calls[calls.length - 1].args[0]
}

test('emptyFilters 的 ExcludeDirs 默认是空数组', () => {
  assert.deepEqual(emptyFilters().ExcludeDirs, [])
})

test('startScan 载荷携带用户添加的排除目录', async () => {
  const { store, stub } = setup()
  store.roots.push('/vol/data')
  store.filters.ExcludeDirs.push('/vol/data/sync', '/vol/data/tmp')
  store.startScan()
  await flush()
  assert.deepEqual(payload(stub).Filters.ExcludeDirs, ['/vol/data/sync', '/vol/data/tmp'],
    '排除目录没有到达后端——扫描会照扫不误')
  stub.restore()
})

const meta = (filters: unknown): HistoryMeta =>
  ({
    id: 1, savedAt: 1, roots: ['/vol'], groups: 0, files: 0, origFiles: 0,
    reclaimable: 0, threads: 2, paranoid: false, filters,
  }) as unknown as HistoryMeta

// 每条用例各自 setup()：startScan 有忙碌互斥（guard），同轮 setup 里第二次重扫
// 会被拦下——拆用例而不是重置 store 状态，才不会把"被 guard 拦了"误读成"透传对了"。

test('历史 filters 缺 ExcludeDirs 键时重扫回填仍是空数组', async () => {
  const { store, stub } = setup()
  // 旧历史行：整个键不存在
  store.rescanHistory(meta({
    IncludeExts: [], ExcludeExts: [], MinSize: 0, MaxSize: 0,
    ExcludePaths: [], IncludeHidden: false, AllowCloudHydration: false,
  }))
  await flush()
  assert.deepEqual(store.filters.ExcludeDirs, [], '缺键未回落到空数组')
  stub.restore()
})

test('历史 ExcludeDirs 为 null（Go 空切片回包）不得污染 store', async () => {
  const { store, stub } = setup()
  store.rescanHistory(meta({ ExcludeDirs: null }))
  await flush()
  assert.deepEqual(store.filters.ExcludeDirs, [], 'null 被原样写进 store（下游 .push 即炸）')
  stub.restore()
})

test('历史里有排除目录时必须带着重扫（整体透传）', async () => {
  const { store, stub } = setup()
  store.rescanHistory(meta({ ExcludeDirs: ['/vol/x'] }))
  await flush()
  assert.deepEqual(payload(stub).Filters.ExcludeDirs, ['/vol/x'],
    '历史里的排除目录没带进重扫载荷')
  stub.restore()
})
