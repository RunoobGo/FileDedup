// M82 防分叉钉（设计稿 §27.2，2026-09-22 裁定「全仓字节单位统一成二进制命名」）：
// 前端这份 humanBytes 与 internal/ops/humanSize、cmd/fdd-cli/humanBytes 三处必须同派。
// 跨 Go/TS 不做运行期互查（M64 已确立「跨语言各留一份 + 各自钉字面量」），所以这里
// 把同一批字节数钉成与 Go 侧两条测试**逐字相同**的单位写法。
// ★ 本文件之前没有这类断言不是漏写：opdisplay.test.ts 里的
// `assert.equal(raw.bytes, humanBytes(n))` 两边调的是同一个函数，属于自参照，钉不住单位串。
import { test } from 'node:test'
import assert from 'node:assert/strict'

import { humanBytes, humanSpeed } from '../src/utils/format'

test('字节单位一律二进制命名：算法本来就是 1024，标签不得写成十进制', () => {
  const cases: Array<[number, string]> = [
    [0, '0 B'],
    [1023, '1023 B'],
    [1024, '1.0 KiB'],
    [1536, '1.5 KiB'],
    // ★ 用 `2 ** n` 而不是 `1 << n`：JS 的位运算是 32 位截断，`1 << 40` 实为 `1 << 8` = 256，
    // 夹具会把自己写成"256 字节期望等于 1.0 TiB"这种永远红不了的用例。
    [2 ** 20, '1.0 MiB'],
    [2 ** 30, '1.0 GiB'],
    [2 ** 40, '1.0 TiB'],
  ]
  for (const [inBytes, want] of cases) {
    assert.equal(humanBytes(inBytes), want, `humanBytes(${inBytes}) 单位分叉`)
  }
})

test('速度沿用同一套单位名：KB/s 与 KiB/s 不能各说各的', () => {
  assert.equal(humanSpeed(1024), '1.0 KiB/s')
  assert.equal(humanSpeed(0), '—')
})
