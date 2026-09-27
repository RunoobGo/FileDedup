// M282：操作前模态与操作后台账**互相矛盾**（真机读数：模态「文件将移入系统回收站，
// 可随时还原。」vs 台账同一批文件「回收站 · 不可回撤」）。两句各自都属实——
// 「可随时还原」说的是 OS 回收站，「不可回撤」说的是应用内回撤——但用户在同一批
// 文件上先后读到，会读成"刚才说能随时还原，现在又说撤不了"。
//
// 四条命题：
//   ① 那句**唯一**让用户以为能一键撤回的话必须消失（反面钉，逐字）；
//   ② "还原"这个动作的主语必须显式是**系统回收站**；
//   ③ 模态里就要预告"本应用不提供一键回撤"，并把"能不能撤"的准绳指到记录页徽标；
//   ④ 这两句不许再分叉：模态正文与徽标 title 同住在 `utils/undoReason.ts`
//      （M79 已把"这条记录为什么不可回撤"的文案收在那儿，M282 是同一族的另一半）。
import { test } from 'node:test'
import assert from 'node:assert/strict'

import {
  TRASH_MODAL_DESC,
  UNDO_CODE_WINDOWS_TRASH,
  undoBlockedTitle,
} from '../src/utils/undoReason'

test('① 模态正文不再出现「可随时还原」这句唯一的一键撤回暗示', () => {
  assert.ok(!TRASH_MODAL_DESC.includes('可随时还原'), `误导句回来了：${TRASH_MODAL_DESC}`)
  assert.ok(!TRASH_MODAL_DESC.includes('随时还原'), `换个说法的同一句暗示：${TRASH_MODAL_DESC}`)
})

test('② 「还原」的主语显式是系统回收站，不是本应用', () => {
  assert.ok(TRASH_MODAL_DESC.includes('系统回收站'), `没点明去哪儿还原：${TRASH_MODAL_DESC}`)
  assert.ok(TRASH_MODAL_DESC.includes('还原'), `没给出还原这个动作：${TRASH_MODAL_DESC}`)
})

test('③ 模态里就预告应用内不提供一键回撤，并把准绳指到记录页徽标', () => {
  assert.ok(TRASH_MODAL_DESC.includes('本应用'), `没说清应用这一侧的边界：${TRASH_MODAL_DESC}`)
  assert.ok(TRASH_MODAL_DESC.includes('不可回撤'), `没有预告记录页会看到的那四个字：${TRASH_MODAL_DESC}`)
  assert.ok(TRASH_MODAL_DESC.includes('徽标'), `没把"能不能撤"的准绳指到用户下一站会看到的东西：${TRASH_MODAL_DESC}`)
})

test('④ 模态正文与徽标 title 同源于一个模块，措辞不得两处各写一遍', () => {
  const badge = undoBlockedTitle(UNDO_CODE_WINDOWS_TRASH)
  assert.ok(badge.includes('应用内回撤') && TRASH_MODAL_DESC.includes('本应用'),
    '两处必须都在说"应用内"这一件事，否则又回到各说各话')
  // 徽标那一句不许被搬进模态（长度与场合不同），但两句里"回收站"这一落点必须同源一致。
  assert.ok(badge.includes('回收站') && TRASH_MODAL_DESC.includes('回收站'))
})
