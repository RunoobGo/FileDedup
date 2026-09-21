// §20 第九批 P-20-4（M83，04 §6.11 FE-10；设计段 §20.0-4 / §20.1-4）。
//
// 缺陷原形（FailedDrawer.vue:21 改前）：
//   navigator.clipboard?.writeText(text).catch((e) => toast.notifyError('复制失败', e))
// 可选链短路时**整个表达式是 undefined**，`.catch` 从未挂上 ⇒ 没有剪贴板的环境
// （非安全上下文、无授权、部分 Linux WebKit 后端）点「复制全部」完全静默：
// 用户以为自己复制到了，粘贴时才发现什么都没有。
//
// ★ 本文件的"改前必红"由变异 M20-d 提供（见设计段 §20.4）：`copyText` 是新符号，
//   改前树 import 它只会得到 ERR_MODULE_NOT_FOUND —— 那是"红在错误的格子上"，
//   不能当作修前必红的证据。第一格因此写成一条**语言语义自检**（改前改后都绿），
//   它钉的是"短路时 .catch 不挂上"这个前提本身，不是本仓库的实现。
import { test } from 'node:test'
import assert from 'node:assert/strict'

import { copyText } from '../src/utils/clipboard'

test('前提自检：可选链短路时 .catch 根本不会挂上（这就是 M83 的静默成因）', async () => {
  let reported = ''
  const clip = undefined as { writeText(t: string): Promise<void> } | undefined
  // 改前那一行的形状，逐字复刻：clip?.writeText(t)?.catch(handler) —— 整体求值为 undefined。
  const expr = clip?.writeText('x')?.catch((e: unknown) => { reported = String(e) })
  assert.equal(expr, undefined, '前提：短路后必须拿到 undefined（不是 Promise），否则这条探针测不到成因')
  assert.equal(reported, '', '前提：短路时 handler 不可能被调用 ⇒ "静默"不是概率问题，是必然')
})

test('没有剪贴板时 copyText 必须抛，而不是静默返回', async () => {
  await assert.rejects(
    () => copyText('payload', undefined),
    /剪贴板/,
    '无剪贴板环境必须给人话错误，交组件弹一条 toast（改前是彻底静默）',
  )
})

test('写入被拒时必须抛并带上原因；成功时不得抛', async () => {
  await assert.rejects(
    () => copyText('payload', { writeText: () => Promise.reject(new Error('NotAllowedError')) }),
    /NotAllowedError/,
    '写入被拒的原因不许被吞掉',
  )
  let written = ''
  await copyText('abc\tdef', {
    writeText: (t: string) => { written = t; return Promise.resolve() },
  })
  assert.equal(written, 'abc\tdef', '成功路径必须原文写入（制表符分隔是排障用的列结构，不许改写）')
})

test('默认取用全局剪贴板时，本机没有 navigator.clipboard 也必须报错（不静默）', async () => {
  // 不注入 clip 参数，走真实取值路径。Node 24 实测 globalThis.navigator 存在但 clipboard 为
  // undefined（且是 getter-only 全局，测试里不能靠改全局造环境），所以这一格正是生产上
  // "没有剪贴板"那一档的真实读数。
  assert.equal(globalThis.navigator?.clipboard, undefined, '前提：本机确实没有剪贴板接口')
  await assert.rejects(() => copyText('payload'), /剪贴板/)
})
