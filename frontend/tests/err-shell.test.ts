// M202 P-5（登记 §6.29，设计稿 `2026-09-24-error-shell-m202` §4）：
// 前端把 `{error, detail}` 载荷拼成一行提示的契约。
//
// ★ "改前必红"这一格由**变异 Vd** 提供（设计稿 §4）：`errWithDetail` 是新符号，
//   改前树 import 它只会得到 ERR_MODULE_NOT_FOUND——那是红在错误的格子上，
//   不能当修前证据。后端侧的真改前红见 `app_error_shell_test.go` 的 P-4
//   （emit 退回原样即红，取数过程记在 04 §6.34）。
//
// 本文件钉的是**拼接次序与降级语义**：中文外壳在前、系统原文在后且标明是原文、
// 两者同值不重复、缺 detail 不冒括号。这些正是"降级为 detail"与"删掉原文"的区别。
import { test } from 'node:test'
import assert from 'node:assert/strict'

import { errWithDetail } from '../src/utils/errShell'

test('M202-A 有原文时：中文外壳在前，原文在后并标明来源', () => {
  const got = errWithDetail({
    error: '权限不足，无法访问该位置',
    detail: 'open /Users/me/Secret/x.bin: permission denied',
  })
  assert.equal(got, '权限不足，无法访问该位置（系统原文：open /Users/me/Secret/x.bin: permission denied）')
  // 次序本身就是判据：外壳必须先出现。
  assert.ok(got.indexOf('权限不足') < got.indexOf('permission denied'), '英文原文不得抢在中文外壳之前')
})

test('M202-B 没有原文时不冒空括号（应用自撰中文错误就是这一形）', () => {
  assert.equal(errWithDetail({ error: '任务进行中，已拒绝本次清理' }), '任务进行中，已拒绝本次清理')
  assert.equal(
    errWithDetail({ error: '该卷只读', detail: '该卷只读' }),
    '该卷只读',
    'detail 与外壳同值时必须省掉，否则用户读两遍同一句',
  )
  assert.equal(errWithDetail({ error: '该卷只读', detail: '' }), '该卷只读')
})

test('M202-C 旧形状仍要认（后端非三处发射点的载荷、以及前端本地异常）', () => {
  assert.equal(errWithDetail('boom'), 'boom', '裸字符串')
  assert.equal(errWithDetail(new Error('本地异常')), '本地异常', 'Error 实例取 message')
  assert.equal(errWithDetail({ detail: '只有原文没有外壳' }), '只有原文没有外壳', '缺 error 时退回 detail，不得返回空串吞掉错误')
  assert.equal(errWithDetail(null), '')
  assert.equal(errWithDetail(undefined), '')
})
