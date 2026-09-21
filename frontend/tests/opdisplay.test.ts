// M15 行为探针：确认框里"这批字节意味着什么"的句子必须与后端字节口径一致。
//
// 为什么能零依赖跑：本机 Node 24 原生剥离 TS 类型（`node --test tests/*.test.ts`），
// 不需要 vitest。仓库里没有 JS 测试运行器，装一个属于依赖决策，未擅自做。
// src/ 里的相对导入省略了扩展名（Vite 的 bundler 解析允许，Node 的 ESM 不允许），
// 所以入口带 tests/register.mjs 补一层解析钩子，见该文件注释。
// 代价是本文件不在 tsconfig 的 include 里（src/**/*），因此 **不受 vue-tsc 类型检查**；
// 它只在 `scripts/test-frontend-logic.sh` 里被执行。
//
// 钉住的口径来源：internal/ops/executor.go 的 aggregate()——
//   hardlink → LinkedBytes、symlink → SymlinkedBytes，两者都**不**计入 Reclaimed。
// 所以这两类（外加同卷 move）在 UI 上说"空间可释放"就是假话；
// 结果条（ResultView.vue 的 opsbar）早就分别措辞，确认框曾经是唯一说错的地方。
import { test } from 'node:test'
import assert from 'node:assert/strict'

// 故意与 src/ 一样省略扩展名：这样钩子本身也被这组用例覆盖，钩子坏了会立刻报错，
// 而不是写成 .ts 后一直"能跑"却再也用不上。
import { reclaimLine } from '../src/utils/opdisplay'
import { humanBytes } from '../src/utils/format'

/** 三段拼成整句，便于按"一句话"来断言。 */
const line = (kind: Parameters<typeof reclaimLine>[0], bytes: number) => {
  const l = reclaimLine(kind, bytes)
  return { raw: l, joined: `${l.lead}${l.bytes}${l.tail}` }
}

test('hardlink 不得声称"可释放"：占用不变才是事实', () => {
  const { joined } = line('hardlink', 5 << 30)
  assert.match(joined, /占用不变/)
  assert.doesNotMatch(joined, /可释放|释放/)
  // 合并后两份路径都还在，这个前提也不能省——用户据此判断"我还能不能打开它"。
  assert.match(joined, /两份路径仍可访问/)
})

test('move 不得笼声称"可释放"：同卷只是改名', () => {
  const { joined } = line('move', 1 << 20)
  assert.match(joined, /同卷只是改名/)
  assert.match(joined, /磁盘总占用不变/)
  assert.doesNotMatch(joined, /可释放/)
})

test('symlink 说清"只保留一份数据"，不借用硬链接的"占用不变"', () => {
  const { joined } = line('symlink', 3 << 20)
  assert.match(joined, /软链接/)
  assert.doesNotMatch(joined, /可释放/)
  // 软链接磁盘上确实少一整份，说"占用不变"同样是对的反方向的假话。
  assert.doesNotMatch(joined, /占用不变/)
})

test('trash / delete 仍说"空间可释放"——数据真的离开磁盘', () => {
  for (const kind of ['trash', 'delete'] as const) {
    const { joined } = line(kind, 2 << 20)
    assert.match(joined, /空间可释放/, `${kind} 应保留"释放"措辞`)
  }
})

test('数字三段式：bytes 段就是 humanBytes 的结果，可被调用方单独加粗', () => {
  const n = 1234567
  const { raw, joined } = line('trash', n)
  assert.equal(raw.bytes, humanBytes(n))
  assert.ok(raw.lead.length > 0 && raw.tail.length > 0)
  assert.ok(joined.indexOf(raw.bytes) > raw.lead.length - 1)
})

test('零字节与非法字节不显示成空句', () => {
  for (const b of [0, -1, NaN]) {
    const { joined } = line('hardlink', b)
    assert.match(joined, /0 B/)
  }
})
