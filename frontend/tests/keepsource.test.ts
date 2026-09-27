// M290/M297 实施批的文案与判据方（`utils/keepsource.ts`）。
//
// 三条命题，一条一条对应"界面上那句假话"：
//   ① 可见文案逐字是用户裁定的那一句（「数据在保留源，需手动放回」）——这是本轮的验收线，
//      漂移一个字就成了另一句承诺；
//   ② 展示判据只把**软链接**那一类撤下「回撤」出口，其余四类（trash/move/hardlink/delete）
//      的既有可见行为一字不变——改多了就是范围蔓延；
//   ③ 悬空行的新说明里**不得再出现**"备份仍在"那句改前的假承诺（反面钉）。
import { test } from 'node:test'
import assert from 'node:assert/strict'

import {
  KEEP_SOURCE_LABEL,
  SYMLINK_KIND,
  danglingKeepTitle,
  keepSourceTitle,
  offersInAppUndo,
  revealKeepTitle,
} from '../src/utils/keepsource'

test('可见文案逐字是用户裁定的那一句', () => {
  assert.equal(KEEP_SOURCE_LABEL, '数据在保留源，需手动放回')
})

test('kind 字面量与后端记账的 kind 逐字相同', () => {
  assert.equal(SYMLINK_KIND, 'symlink')
})

test('判据：只有软链接撤下「应用内回撤」出口，其余四类原样保留', () => {
  assert.equal(offersInAppUndo('symlink'), false)
  for (const kind of ['trash', 'move', 'hardlink', 'delete']) {
    assert.equal(offersInAppUndo(kind), true, `${kind} 的回撤出口不该被这一批改掉`)
  }
})

test('悬空行说明：不再写"备份仍在"，同时给出保留源与手动放回', () => {
  const t = danglingKeepTitle('E:\\keep\\dup.bin')
  assert.ok(!t.includes('备份仍在'), `改前的假承诺回来了：${t}`)
  assert.ok(!t.includes('点右侧「回撤」'), `悬空行仍把用户指向一个不存在的按钮：${t}`)
  assert.ok(t.includes('E:\\keep\\dup.bin'), '必须把保留源路径交给用户')
  assert.ok(t.includes('手动'), '恢复方式必须是手动放回')
})

test('徽标与按钮的悬浮说明各自点名对方的存在（用户要能找到出口）', () => {
  assert.ok(keepSourceTitle().includes('直达保留原目录'), '徽标说明必须点名那个按钮')
  assert.ok(revealKeepTitle('E:\\keep').includes('E:\\keep'), '按钮说明必须带上真实目标路径')
  assert.ok(revealKeepTitle('E:\\keep').includes('不代劳'), '放回动作的后果要在说明里如实写')
})
