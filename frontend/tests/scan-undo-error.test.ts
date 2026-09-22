// §28.1 M79 的 store 腿探针（裁定：判据归后端、文案归前端）。
//
// 为什么这一条是正牌「改前当场红」：改后后端两条出口（app.go:2164 / :2308）下发的
// error 串是**码**（undo-code-windows-trash / undo-code-permanent-delete），不再是中文正文。
// 把 store 收到的串换成那个形状，改前树里没有任何一层认识它 ⇒ toast 正文就是
// 「回撤失败：undo-code-windows-trash」——用户读到的是内部标识符。改后必须是中文、
// 且**不含**那串裸码。这一格正是"只搬后端、忘了搬前端"会留下的形状。
//
// 第三条用例是**负控制**（改前改后都为真，不是修前红）：非码的 error 必须原样透传，
// 否则这条翻译层会把 undoItem 失败链上其它错误（在途、记录不存在…）统统改写成回撤解释。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createPinia, setActivePinia } from 'pinia'

import { installWailsStub, flush } from './harness.mjs'
import { useScanStore } from '../src/stores/scan'
import { useToastStore } from '../src/stores/toast'

/** 让指定后端方法回绝成给定字符串（Wails 的字符串错误经 Error 承载）。 */
function rejecting(method: string, msg: string) {
  return { [method]: () => Promise.reject(new Error(msg)) }
}

function lastToast(): string {
  const toasts = useToastStore().toasts
  return toasts.length ? toasts[toasts.length - 1].msg : ''
}

test('undoRecord：后端下发原因码时，toast 正文是中文而不是裸码', async () => {
  setActivePinia(createPinia())
  const stub = installWailsStub(rejecting('UndoOperation', 'undo-code-windows-trash'))
  const scan = useScanStore()

  await scan.undoRecord(7)
  await flush()

  const msg = lastToast()
  assert.ok(
    !msg.includes('undo-code-windows-trash'),
    `toast 里漏出了后端原因码（前端没翻译）：${msg}`,
  )
  assert.ok(msg.includes('回收站'), `windows 回收站那一格该给出回收站引导：${msg}`)
  assert.ok(msg.includes('还原'), `文案的「下一步」半句不得丢：${msg}`)
  stub.restore()
})

test('undoItem：单条回撤同样把码翻成中文', async () => {
  setActivePinia(createPinia())
  const stub = installWailsStub(rejecting('UndoOperationItem', 'undo-code-permanent-delete'))
  const scan = useScanStore()

  await scan.undoItem(7, 3)
  await flush()

  const msg = lastToast()
  assert.ok(!msg.includes('undo-code-permanent-delete'), `toast 里漏出了后端原因码：${msg}`)
  assert.ok(msg.includes('永久删除'), `永久删除那一格该说明物理不可恢复：${msg}`)
  assert.ok(msg.includes('移入回收站'), `文案的「替代方案」半句不得丢：${msg}`)
  stub.restore()
})

test('负控制：不是原因码的 error 原样透传（改前改后都应为真）', async () => {
  setActivePinia(createPinia())
  const stub = installWailsStub(rejecting('UndoOperation', '回撤在途：上一条还没结束'))
  const scan = useScanStore()

  await scan.undoRecord(9)
  await flush()

  assert.equal(lastToast(), '回撤失败：回撤在途：上一条还没结束')
  stub.restore()
})
