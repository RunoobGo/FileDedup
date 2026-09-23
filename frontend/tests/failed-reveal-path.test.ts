// 功能 4（2026-09-23）：失败清单的逐行「打开文件 / 打开所在目录」——前端这一腿。
//
// 这一条链上前端只做两件事：把路径**原样**交给后端、把后端的拒绝**原样**报给用户。
// 两件事各有一个对应的失效形态，所以各有一条用例：
//   - 交接变形：截断、trim 之外的归一化、大小写改写，都会让"打开"落到另一个文件上；
//     失败清单里同名的东西本就多，打开错了比打不开更糟。
//   - 静默吞掉：M83 那一族（"点了没反应"）在按钮上的复现。判据（路径能不能开、
//     是文件还是目录）**全在后端**（AS-H6），前端不许自己先看一眼 path 再决定发不发——
//     所以这里连"空白路径也照样发出去"都钉住了：用户要看见后端给的原因，
//     不是一条什么都不说的 toast。
//
// 组件那一腿（FailedDrawer.vue 真的去调这两个包装）由 scripts/test-frontend-logic.sh
// 的接线锚钉住：node 用例挂不起组件，锚标识符是这里的既有分工（同 GroupCard / ResultView）。
import { test } from 'node:test'
import assert from 'node:assert/strict'

import { installWailsStub } from './harness.mjs'
import { api } from '../src/wails'

const DIRTY = '/tmp/失败清单 a; echo pwned.bin' // 空格 + shell 元字符：不许被拆或被改

function setup(responders: Record<string, any> = {}) {
  const stub = installWailsStub({
    RevealPath: () => undefined,
    OpenPath: () => undefined,
    ...responders,
  })
  return { stub }
}

const argOf = (stub: any, method: string) => {
  const calls = stub.calls.filter((c: any) => c.method === method)
  assert.equal(calls.length, 1, `${method} 应恰好调用一次，实际 ${calls.length} 次`)
  return calls[0].args
}

test('R1 revealPath 走 RevealPath，路径逐字节原样送达', async () => {
  const { stub } = setup()
  await api.revealPath(DIRTY)
  assert.deepEqual(argOf(stub, 'RevealPath'), [DIRTY], '交接变形：后端收到的不是界面显示的那条路径')
  stub.restore()
})

test('R2 openPath 走 OpenPath，与 revealPath 是两个不同绑定（不是一个方法的别名）', async () => {
  const { stub } = setup()
  await api.openPath(DIRTY)
  assert.deepEqual(argOf(stub, 'OpenPath'), [DIRTY])
  assert.equal(stub.invoked('RevealPath'), 0, '打开文件本身不该顺带弹一个"选中"窗')
  stub.restore()
})

test('R3 后端拒绝必须抛回调用方，组件才有的 toast 可弹（不静默）', async () => {
  const { stub } = setup({ RevealPath: () => Promise.reject(new Error('路径不存在或无法访问')) })
  await assert.rejects(() => api.revealPath('/tmp/gone.bin'), /路径不存在/)
  stub.restore()
})

test('R4 空白路径也照样送出去：能不能开是后端的判据，前端不预先拦成静默', async () => {
  const { stub } = setup({ OpenPath: () => Promise.reject(new Error('路径为空')) })
  await assert.rejects(() => api.openPath('   '), /路径为空/)
  assert.deepEqual(argOf(stub, 'OpenPath'), ['   '], '前端不得替后端"猜"这条路径没救就不发')
  stub.restore()
})
