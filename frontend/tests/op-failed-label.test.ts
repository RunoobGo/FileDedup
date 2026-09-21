// §20 第九批 P-20-3（M81，04 §6.11 FE-8；设计段 §20.0-3 / §20.1-3）。
//
// 缺陷原形：四个开失败抽屉的入口里三处取 `store.failed.length`（全量：扫描期 ∪ 本次操作，
// 后端 app.go:1960 是并集），只有结果横幅 ResultView.vue:459 取 `store.opsResult.Failed.length`
// （本次）⇒ 按钮上写"失败 2（查看）"，点进去抽屉标题是"失败清单（7）"，
// 而正文列的正是那 7 条。用户读到的是"数字对不上"，界面上没有一句话说明这是两个范围。
//
// ★ 登记的修法（"都取 opsResult.failed 长度"）**不采纳**，理由见 §20.0-3：那会让抽屉标题
//   与自己列出的正文不同源，并把扫描期失败项从计数里抹掉。本批改判为"各说各的范围"。
//
// ★ 本文件的"改前必红"由变异 M20-c 提供：`opFailedLabel` 是新符号，改前树 import 它
//   只会得到"没有这个导出"，那是红在错误的格子上，不能当修前证据。
import { test } from 'node:test'
import assert from 'node:assert/strict'

import { opFailedLabel } from '../src/utils/opdisplay'

test('两个数不等时文案必须限定为"本次"（横幅报本次、抽屉列全量）', () => {
  assert.equal(opFailedLabel(2, 5), '本次失败 2（查看）')
  assert.ok(
    opFailedLabel(2, 5).startsWith('本次'),
    `清单里还有 3 条不是这次操作产生的，不限定范围就是一句假话：实测 ${opFailedLabel(2, 5)}`,
  )
})

test('两个数相等时不加"本次"限定（那时它就是全量，加限定反而暗示还有别的数）', () => {
  assert.equal(opFailedLabel(5, 5), '失败 5（查看）')
  assert.ok(!opFailedLabel(5, 5).includes('本次'), `实测 ${opFailedLabel(5, 5)}`)
})

test('本次零失败 ⇒ 不给文案（按钮本身就不该出现，绝不能凭全量非空而亮）', () => {
  assert.equal(opFailedLabel(0, 5), '')
  assert.equal(opFailedLabel(0, 0), '')
})

test('全量少于本次读数（后端并集不可能如此，出现即为状态错乱）⇒ 按相等处理，不编出"本次"故事', () => {
  assert.equal(opFailedLabel(3, 2), '失败 3（查看）')
})
