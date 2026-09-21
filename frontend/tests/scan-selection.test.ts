// M18（2026-09-21 全仓审计 §五 18）：勾选数量的判据只留一份。
//
// 缺陷原形：GroupCard 组内全选框的 aria-label 自己算 `files.length - 1` 当"冗余项数"，
// 而后端在 I4 场景（组内一个保留项都没有——例如保留策略被重置）下真的会一个都不标，
// 于是标签少报 1 项。aria-label 是屏幕阅读器唯一的说明来源，等于只对不可见的用户说假话。
// 修正方向不是把公式改对，而是**取消这份第二实现**：数几项由 store.groupSelCount 回答，
// 它和"点这一下实际勾上哪些项"用的是同一个 groupSelCandidates。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createPinia, setActivePinia } from 'pinia'

import { installWailsStub } from './harness.mjs'
import { useScanStore } from '../src/stores/scan'

const file = (id: number, isKeep = false) => ({
  id,
  path: `/vol/a/f${id}.bin`,
  name: `f${id}.bin`,
  size: 1024,
  mtime: 0,
  isKeep,
  volume: 'v1',
  volumeResolved: true,
})
const group = (groupID: number, files: any[]) => ({ groupID, reclaimable: 0, size: 0, files })

function setup(groups: any[] = []) {
  setActivePinia(createPinia())
  const stub = installWailsStub({
    GetResultGroups: () => ({ groups: structuredClone(groups), total: groups.length, totalReclaimable: 0 }),
    GetFailedItems: () => [],
    GetStatus: () => 'Idle',
    GetSettings: () => ({ theme: 'light' }),
    GetVersion: () => '0.5.0',
    GetStartupNotice: () => '',
    ListScanHistory: () => [],
  })
  const store = useScanStore()
  store.bindEvents()
  store.groups = groups
  return { store, stub }
}

test('I4：组内 0 个保留项时，冗余项数等于文件数（曾按 length-1 少报 1）', () => {
  const g = group(1, [file(11), file(12), file(13)])
  const { store, stub } = setup([g])
  assert.equal(store.groupSelCount(g), 3)
  assert.equal(store.groupSelState(g), 'none')
  stub.restore()
})

test('有保留项时冗余项数 = 非保留项数；全是保留项则为 0（没有可勾项）', () => {
  const one = group(2, [file(21, true), file(22), file(23)])
  const all = group(3, [file(31, true), file(32, true)])
  const { store, stub } = setup([one, all])
  assert.equal(store.groupSelCount(one), 2)
  assert.equal(store.groupSelCount(all), 0)
  assert.equal(store.groupSelState(all), 'none', '全是保留项时不能报成"已全选"')
  stub.restore()
})

test('计数与"点下去实际勾上什么"必须同源：勾选后状态由 none→some→all', () => {
  const g = group(4, [file(41, true), file(42), file(43)])
  const { store, stub } = setup([g])
  const candidates = store.groupSelCount(g)
  assert.equal(store.groupSelState(g), 'none')
  store.toggleSelect(42)
  assert.equal(store.groupSelState(g), 'some')
  store.toggleSelect(43)
  assert.equal(store.groupSelState(g), 'all')
  assert.equal(store.selection.size, candidates)
  // 保留项就算被硬塞进 selection 也不该改变判据口径——它永远不在候选集里。
  store.toggleSelect(41)
  assert.equal(store.groupSelState(g), 'all')
  stub.restore()
})

test('组内全选只勾冗余项；再点一次整组取消', () => {
  const g = group(5, [file(51, true), file(52), file(53), file(54)])
  const { store, stub } = setup([g])
  store.toggleGroupSelection(g)
  assert.deepEqual([...store.selection].sort((a, b) => a - b), [52, 53, 54])
  store.toggleGroupSelection(g)
  assert.equal(store.selection.size, 0)
  stub.restore()
})

test('全选覆盖整个结果集的全部冗余项（含折叠组——范围写在按钮 title 上）', () => {
  const g1 = group(6, [file(61, true), file(62)])
  const g2 = group(7, [file(71), file(72), file(73)])
  const { store, stub } = setup([g1, g2])
  store.selectAll()
  assert.deepEqual([...store.selection].sort((a, b) => a - b), [62, 71, 72, 73])
  assert.equal(store.selectedFiles.length, 4)
  assert.equal(store.selectedBytes, 4 * 1024)
  stub.restore()
})
