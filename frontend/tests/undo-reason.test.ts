// M79 文案方的行为钉（设计段 §28.1；裁定"判据归后端、文案归前端"）。
//
// 这里只测 `utils/undoReason.ts` 这一份**纯逻辑**（无 .vue、无 pinia），故走 node --test。
// 三条"逐字相同"是本条的验收线：M79 是**搬家**不是改写，所以新文案方必须与它替换掉的
// 两处原文一字不差——toast 体 == 原 app.go 那两句，title 体 == 原 RecordsView 那两句。
// 期望值写在这里而不是"从被测模块读回来自己比自己"，否则搬家途中丢了半句也测不出来。
import { test } from 'node:test'
import assert from 'node:assert/strict'

import {
  UNDO_CODE_PERMANENT_DELETE,
  UNDO_CODE_WINDOWS_TRASH,
  undoBlockedText,
  undoBlockedTitle,
  undoTitleCodeForKind,
} from '../src/utils/undoReason'

// —— 后端契约：码的字符串本身不许漂移（app.go 的 undoCode* 与此逐字相同）——
test('原因码字面量与后端契约一致', () => {
  assert.equal(UNDO_CODE_WINDOWS_TRASH, 'undo-code-windows-trash')
  assert.equal(UNDO_CODE_PERMANENT_DELETE, 'undo-code-permanent-delete')
})

test('toast 体：Windows 回收站那一码给出「原因 + 手动出路」两半，逐字等于原后端文案', () => {
  assert.equal(
    undoBlockedText(UNDO_CODE_WINDOWS_TRASH),
    'Windows 回收站操作不支持应用内回撤：系统 API 不返回' +
      '「每个文件落在回收站的哪个位置」的映射，应用无法定位文件而把它搬回原处。' +
      '文件本身仍在回收站里，请点上方「打开系统回收站」，右键选择「还原」即可取回。',
  )
})

test('toast 体：永久删除那一码同样两半都在，逐字等于原后端文案', () => {
  assert.equal(
    undoBlockedText(UNDO_CODE_PERMANENT_DELETE),
    '永久删除不支持回撤：文件已从磁盘移除，没有任何可恢复的来源。' +
      '若还需保留这些文件，请重新扫描后改用「移入回收站」或「移动」。',
  )
})

test('title 体：两码各得一句，逐字等于原 RecordsView 文案', () => {
  assert.equal(
    undoBlockedTitle(UNDO_CODE_WINDOWS_TRASH),
    'Windows 回收站不支持应用内回撤：系统 API 不返回每个文件的落点映射，' +
      '应用无法定位后搬回原处。文件仍在回收站里，请用「打开系统回收站」右键「还原」。',
  )
  assert.equal(
    undoBlockedTitle(UNDO_CODE_PERMANENT_DELETE),
    '永久删除不支持回撤：文件已从磁盘移除，没有可恢复的来源。' + '若仍需保留，请改用「移入回收站」或「移动」。',
  )
})

// 后端加了新码而前端没跟，是唯一一种"两边各自都没错、用户却看到空说明"的形状 ⇒ 兜底必须点名。
test('未知码：toast 兜底含那个码串（看得见才知道漏了文案）', () => {
  const out = undoBlockedText('undo-code-future-kind')
  assert.equal(out, 'undo-code-future-kind')
})

test('非码的 error 原样透传（回撤链上其它失败不得被改写成回撤解释）', () => {
  assert.equal(undoBlockedText('回撤在途：上一条还没结束'), '回撤在途：上一条还没结束')
  assert.equal(undoBlockedText(''), '')
})

test('kind → 码：trash 走回收站那一码，其余走永久删除那一码', () => {
  assert.equal(undoTitleCodeForKind('trash'), UNDO_CODE_WINDOWS_TRASH)
  assert.equal(undoTitleCodeForKind('delete'), UNDO_CODE_PERMANENT_DELETE)
  assert.equal(undoTitleCodeForKind('hardlink'), UNDO_CODE_PERMANENT_DELETE)
})
