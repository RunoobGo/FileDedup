// M291：台账「去向」列对链接类条目说的是**哪一时刻**的事实。
//
// 缺陷形状（真机读数 04 §6.50 一 / W3-6 同场）：手工把 `group_02` 位置上的软链接
// 从"指向 keep.bin"改指到同目录的 `third.bin`，记录页那一行仍然显示
// `软链接 → …\g02\keep.bin`——它取的是**记账那一刻**写进账本的 `linkSrc`，
// 而 `→` 与"当前指向"同形 ⇒ 一条已被第三方改指的链接在台账上看起来完全健康。
// 执行侧（`verifySymlinked`）当时是**拦住**的（交回「标识不符」），所以这不是数据
// 安全问题，而是显示口径与现状脱节；两者口径不一致会让用户以为台账可信。
//
// 取向（登记原文）：链接类条目的「去向」要么现读一次，要么在文案上如实标成
// "记录中的目标"。★ 本轮选后者——现读要新增一条 IPC 并在列表每行发一次 stat，
// 而 M291 的边界明写"不主张改指后清扫会出错（执行侧已拦）"，为一个显示口径
// 付一次逐行 IPC 不值。
//
// 两条钉各自的方向：
//   ① 正向：两档链接类都带"记录中的目标"这一口径标注；
//   ② 反面：与现状同形的裸箭头写法（`软链接 → X`）不得复现，视图也不许自己内联造句。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

import { RECORDED_TARGET_LABEL, recordedDest } from '../src/utils/keepsource'

const src = readFileSync(new URL('../src/views/RecordsView.vue', import.meta.url), 'utf8')
const has = (re: RegExp, why: string) => assert.ok(re.test(src), `${why}（锚：${re}）`)
const lacks = (re: RegExp, why: string) => assert.ok(!re.test(src), `${why}（被禁写法：${re}）`)

test('口径标注逐字是「记录中的目标」，链接两档都带上它', () => {
  assert.equal(RECORDED_TARGET_LABEL, '记录中的目标')
  for (const symlink of [true, false]) {
    const got = recordedDest('E:\\g02\\keep.bin', symlink)
    assert.ok(got.includes(RECORDED_TARGET_LABEL), `缺口径标注：${got}`)
    assert.ok(got.includes('E:\\g02\\keep.bin'), `目标路径被丢了：${got}`)
  }
  // 两档仍要分得开（改前就是靠 isSymlink 区分的，不许折成一句）。
  assert.ok(recordedDest('X', true).startsWith('软链接'), '软链接一档的字样丢了')
  assert.ok(recordedDest('X', false).startsWith('硬链接'), '硬链接一档的字样丢了')
})

test('反面钉：不得再用与"当前指向"同形的裸箭头写法', () => {
  // 真机读数点名的正是这个形状：`软链接 → …\keep.bin`。
  assert.ok(!/^软链接 → /.test(recordedDest('E:\\g02\\keep.bin', true)), '仍是现状同形写法')
  assert.ok(!/^硬链接 → /.test(recordedDest('E:\\g02\\keep.bin', false)), '仍是现状同形写法')
})

test('视图的「去向」列问那一家文案方，不自己内联造句', () => {
  has(/recordedDest\(/, '链接类去向没引用集中出口')
  // 改前那两句就在视图里，残留一份算没改净（本仓的 M79 纪律：判据与文案各归一处）。
  lacks(/软链接 → \$\{/, '视图里还在内联"软链接 → 路径"')
  lacks(/链接到 \$\{/, '视图里还在内联"链接到 路径"')
})

test('去向列的表头与行内标注不互相打脸（列名不得自称现读）', () => {
  // 表头只说"去向"这一中性词；一旦有人把它改成"当前位置/当前指向"这类
  // 现在时说法，行内那句"记录中的目标"就自相矛盾了。
  lacks(/当前位置|当前指向/, '表头/说明用了现在时说法，与「记录中的目标」冲突')
})
