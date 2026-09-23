// R3-2（2026-09-23 第四轮全仓审查，设计段 §30.8）：前端唯一保留的卷判据零覆盖。
//
// 缺陷原形不是"判据写错了"——M7（2026-09-20）已经把它改对了；是**改对之后没有任何用例站在
// 那条判据上**。于是"改回 M7 改前那句"在今天仍然是一记无人可挡的变异，而这一格的代价
// 是方向性的：假阳性会让界面对一个同卷组显示「软链接合并」，用户在未提权时点进去
// 只能收一串"需要权限"（ResultView.vue:25-30 把这条写得很清楚）。
//
// ★ 用例方向全部取自现实现（pathpolicy.ts:20），不是取自审查报告的用例清单。
//   审查原文要的是"Windows 盘符 / UNC / 尾分隔符 / 相对路径"混例——那是 D-1（AS-H6）
//   删掉前端那套路径判据之前的考题；本函数的输入是后端算好的卷标识
//   （app.go fileVolume 的 `vid:<id>` / `root:<VolumeName>`），函数体不解析路径。
//   所以盘符那一格（U-6）保留，但钉的是"连盘符形状也不许猜"，不是"盘符怎么比"。
import { test } from 'node:test'
import assert from 'node:assert/strict'

import { isGroupCrossVolume } from '../src/utils/pathpolicy'

const f = (volume: string, resolved = true) => ({ volume, volumeResolved: resolved })

test('U-1 空组：判同卷且不抛（少了 length 短路就是 TypeError）', () => {
  assert.equal(isGroupCrossVolume([]), false)
})

test('U-2 单成员组：无从跨卷', () => {
  assert.equal(isGroupCrossVolume([f('vid:7')]), false)
})

test('U-3 三成员同卷：不显示跨卷入口', () => {
  assert.equal(isGroupCrossVolume([f('vid:7'), f('vid:7'), f('vid:7')]), false)
})

test('U-4 两成员卷不同且都可信：必须判出跨卷（假阴性方向）', () => {
  assert.equal(isGroupCrossVolume([f('vid:7'), f('vid:8')]), true)
})

test('U-5 未解析那位排在非首位：仍按同卷处理（★ M7 那一格）', () => {
  // M7 改前写作 some(f => !f.volumeResolved || f.volume !== first)：
  // 首位可信、**其他**成员缺身份时反而判成跨卷 ⇒ 正是注释禁止的假阳性。
  assert.equal(isGroupCrossVolume([f('vid:7', true), f('root:D:', false)]), false)
})

test('U-6 两个盘符形状但未解析：前端不解释内容，也不猜', () => {
  assert.equal(isGroupCrossVolume([f('root:C:', false), f('root:D:', false)]), false)
})

test('U-7 判据与成员顺序无关：未解析那位在 0 / 1 / 2 三格同果', () => {
  const bad = f('root:E:', false)
  const a = f('vid:7')
  const b = f('vid:8')
  assert.equal(isGroupCrossVolume([bad, a, b]), false, '未解析在首位')
  assert.equal(isGroupCrossVolume([a, bad, b]), false, '未解析在中间')
  assert.equal(isGroupCrossVolume([a, b, bad]), false, '未解析在末尾')
})

test('U-8 差异藏在中间或末尾都得抓到（防 some 被换成 every）', () => {
  assert.equal(isGroupCrossVolume([f('A'), f('A'), f('B')]), true, '[A,A,B]')
  assert.equal(isGroupCrossVolume([f('B'), f('A'), f('A')]), true, '[B,A,A]')
  assert.equal(isGroupCrossVolume([f('A'), f('B')]), true, '[A,B]')
  assert.equal(isGroupCrossVolume([f('B'), f('A')]), true, '[B,A]（与上一条同果 ⇒ 不依赖 first 是谁）')
})

test('U-9 保守优先：只要有一位缺可信身份，整组按同卷', () => {
  // 这一格是 U-4 与 U-5 的合取——两位已解析的成员**确实**跨卷，但第三位没身份，
  // 于是宁可少一个入口（用户仍可先移动再处理），也不把人选进必然失败的流程。
  assert.equal(isGroupCrossVolume([f('vid:7'), f('vid:8'), f('root:C:', false)]), false)
})
