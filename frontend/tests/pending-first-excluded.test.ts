// C3 / R-前端-4（2026-09-24 第五轮审查）：拟处理清单分页分界的边界数学。
//
// 缺陷原形：PendingDrawer 的 firstExcluded 内联算 `idx = pendingCount - pageStart`，
// 旧式 `idx >= 0 && idx < rows.length ? idx : -1` 在 **idx < 0**（pendingCount <
// pageStart，即将处理段全在更早的页、当前整页都是排除行）时返回 -1 ⇒ 不挂"以下均
// 不会被处理"标题，用户看到一屏灰行却无解释。抽进 utils/pending 后逐格钉住。
import { test } from 'node:test'
import assert from 'node:assert/strict'

import { firstExcludedIndex } from '../src/utils/pending'

const page = (over: Partial<Parameters<typeof firstExcludedIndex>[0]> & { rows: unknown[] }) =>
  ({ page: 0, pageSize: 100, pendingCount: 0, ...over }) as never

test('R-前端-4：整页都在排除段（pendingCount < pageStart）→ 标题置顶（0），不是 -1', () => {
  // 第 2 页（pageStart=100），但将处理项只有 30 个 ⇒ 本页全是排除行。
  const d = page({ page: 1, pageSize: 100, pendingCount: 30, rows: new Array(100).fill(0) })
  assert.equal(firstExcludedIndex(d), 0, '整页排除时必须在页首挂标题（旧实现返回 -1 → 漏标题）')
})

test('R-前端-4：pendingCount==0 且有行 → 0（全部不会被处理）', () => {
  const d = page({ page: 0, pageSize: 100, pendingCount: 0, rows: new Array(5).fill(0) })
  assert.equal(firstExcludedIndex(d), 0)
})

test('R-前端-4：页内分界（前半将处理、后半排除）→ 返回分界下标', () => {
  // pageStart=0，pendingCount=3 ⇒ 前 3 行将处理，第 4 行起排除。
  const d = page({ page: 0, pageSize: 100, pendingCount: 3, rows: new Array(10).fill(0) })
  assert.equal(firstExcludedIndex(d), 3)
})

test('R-前端-4：跨页页内分界（pending 段在本页中途结束）', () => {
  // pageStart=100，pendingCount=105 ⇒ 本页前 5 行将处理，第 6 行起排除。
  const d = page({ page: 1, pageSize: 100, pendingCount: 105, rows: new Array(100).fill(0) })
  assert.equal(firstExcludedIndex(d), 5)
})

test('R-前端-4：整页都是将处理段（idx >= rows.length）→ -1（本页无排除项）', () => {
  const d = page({ page: 0, pageSize: 100, pendingCount: 100, rows: new Array(100).fill(0) })
  assert.equal(firstExcludedIndex(d), -1)
})

test('R-前端-4：空页 / 无数据 → -1', () => {
  assert.equal(firstExcludedIndex(page({ pendingCount: 5, rows: [] })), -1)
  assert.equal(firstExcludedIndex(null), -1)
  assert.equal(firstExcludedIndex(undefined), -1)
})
