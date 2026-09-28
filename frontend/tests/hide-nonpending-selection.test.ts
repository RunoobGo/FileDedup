// M342（2026-09-28 第七轮补审 P0）：翻「隐藏非拟处理项」开关**不得抹掉勾选**。
//
// 改前的形状：toggleHideNonPending → refreshPendingProjection → loadResultPage(false)
// → resetSelection() ⇒ 勾选被静默清空，且**不可回溯**（行被藏起来了，用户没法
// 发现"已选 N"少了谁，也没法取消勾选）。这与 09 §6.1 / 10 §2.4 明写的
// "纯显示层、勾选一字不变"相反——本开关是**显示偏好**，不是"结果集换了"。
import { test } from 'node:test'
import assert from 'node:assert/strict'

import { replaySelection } from '../src/utils/selection'

const page = (rows: Array<[number, boolean?]>) => [{ files: rows.map(([id, isKeep]) => ({ id, isKeep })) }]

test('① 仍可见且可勾的行：勾选一字不变（手册承诺的那半边）', () => {
  const before = new Set([11, 22, 33])
  const after = replaySelection(before, page([[11], [22], [33], [44]]))
  assert.deepEqual([...after].sort(), [11, 22, 33])
})

test('② 这一轮被隐藏的行：不再背着勾（"已选 N"不替看不见的东西说话）', () => {
  const before = new Set([11, 22, 33])
  // 重取后 33 已被隐藏（不在结果页里）
  const after = replaySelection(before, page([[11], [22]]))
  assert.deepEqual([...after].sort(), [11, 22])
  assert.ok(!after.has(33), '被隐藏的行仍背着勾 ⇒ 计数替看不见的东西说话')
})

test('③ 保留项（isKeep）即便在快照里也不回放——它本来就不可勾', () => {
  const before = new Set([11, 22])
  const after = replaySelection(before, page([[11], [22, true]]))
  assert.deepEqual([...after], [11])
})

test('④ 空快照与空结果页：不抛错、得空集（防御性形状）', () => {
  assert.equal(replaySelection(new Set(), page([[11]])).size, 0)
  assert.equal(replaySelection(new Set([11]), []).size, 0)
})

test('⑤ 全部被隐藏时得空集——与"全部清空"同形，但那是对的：没有可见行可留', () => {
  const after = replaySelection(new Set([11, 22]), page([[99]]))
  assert.equal(after.size, 0)
})
