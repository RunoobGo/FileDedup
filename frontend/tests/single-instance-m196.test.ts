// M196 P-5（登记表 APP-23，设计稿 `2026-09-24-single-instance-m196` §3）：
// 第二实例合并到现窗口时的前端提示接线。
//
// 后端（app_single_instance.go）在把窗口前置**之后**才发 app:second-instance，
// 所以提示一定落在已经被带到前面的那个窗口上。本文件钉三格：
//   A 提示存在且说清"是合并、不是坏了"——用户第二次双击图标时看到的就是这一句；
//   B 载荷里的启动目录要出现在提示里（后端原样交回，前端不加工也不丢）；
//   C 载荷形状不对时不得冒出 undefined/null 文本（后端事件面不止这一形，
//     且 C12 的解绑-重挂路径上可能拿到空载荷）。
//
// ★ 本批不宣称验证了"窗口真的到前台"：那一格要靠真机双开 GUI，CI 三腿都不起 GUI
//   （见 §6.36 的"未兑现"清单）。这里只钉事件接上了、话说对了。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createPinia, setActivePinia } from 'pinia'

import { installWailsStub } from './harness.mjs'
import { useScanStore } from '../src/stores/scan'
import { useToastStore } from '../src/stores/toast'

function lastToast(): string {
  const toasts = useToastStore().toasts
  return toasts.length ? toasts[toasts.length - 1].msg : ''
}

test('M196-A 第二实例合并时给出中文提示，且说清是"合并"而不是坏了', () => {
  setActivePinia(createPinia())
  const stub = installWailsStub()
  const scan = useScanStore()
  const unbind = scan.bindEvents()

  stub.emit('app:second-instance', { args: [], workingDirectory: '' })

  const got = lastToast()
  assert.ok(got, 'app:second-instance 没有产生任何提示 ⇒ 用户只会看到"窗口跳了一下，什么都没发生"')
  assert.ok(got.includes('合并'), `提示必须说明另一次启动被合并进来了：${got}`)
  assert.ok(got.includes('FileDedup'), `要点名是哪个程序：${got}`)
  assert.ok(!/undefined|null/.test(got), `提示里漏出了未定义值：${got}`)
  unbind()
  stub.restore()
})

test('M196-B 载荷里的启动目录原样进提示（后端不加工，前端也不丢）', () => {
  setActivePinia(createPinia())
  const stub = installWailsStub()
  const scan = useScanStore()
  const unbind = scan.bindEvents()

  stub.emit('app:second-instance', { args: ['--x'], workingDirectory: '/Users/me/Downloads' })

  const got = lastToast()
  assert.ok(got.includes('/Users/me/Downloads'), `被合并那次的来源目录要可见：${got}`)
  unbind()
  stub.restore()
})

test('M196-C 解绑后不再收（C12 同一姿势：HMR 重挂不得把提示弹两遍）', () => {
  setActivePinia(createPinia())
  const stub = installWailsStub()
  const scan = useScanStore()
  const unbind = scan.bindEvents()

  unbind()
  const before = useToastStore().toasts.length
  stub.emit('app:second-instance', { args: [], workingDirectory: '/tmp' })
  assert.equal(useToastStore().toasts.length, before, '已解绑却仍弹提示 ⇒ 重挂时会重复弹')
  stub.restore()
})
