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
//   hardlink → LinkedBytes、symlink → SymlinkedBytes，两者都**不**计入 Reclaimed；
//   trash → TrashedBytes（2026-09-21 M22 起），同样不计入 Reclaimed。
// 所以这几类（外加同卷 move）在 UI 上说"空间可释放"就是假话；
// 结果条（ResultView.vue 的 opsbar）早就分别措辞，确认框曾经是唯一说错的地方。
import { test } from 'node:test'
import assert from 'node:assert/strict'

// 故意与 src/ 一样省略扩展名：这样钩子本身也被这组用例覆盖，钩子坏了会立刻报错，
// 而不是写成 .ts 后一直"能跑"却再也用不上。
import { reclaimLine, percentOf } from '../src/utils/opdisplay'
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

test('delete 仍说"空间可释放"——数据真的离开磁盘', () => {
  const { joined } = line('delete', 2 << 20)
  assert.match(joined, /空间可释放/)
})

// 2026-09-21（M22，04 §6.8.8）：trash 从上面那条用例里**拆出来**。理由不是"让门禁变绿"，
// 而是原断言钉错了语义：回收站里的文件仍在磁盘上（同卷只是一次改名，跨卷只是搬到另一个
// 卷的回收站），"空间可释放"对它是假话，还与紧挨着的"打开回收站"按钮自相矛盾。
// 后端同一批把 trash 从 Reclaimed 拆进了独立的 TrashedBytes。
test('trash 改说"移入回收站"：清空之前空间并未释放', () => {
  const { joined } = line('trash', 2 << 20)
  assert.match(joined, /移入回收站/)
  assert.match(joined, /清空回收站后才真正释放/)
  assert.doesNotMatch(joined, /可释放/)
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

// ---------- M17：进度条宽度 ----------

test('percentOf 把比值折成 0–100，越界一律钳住', () => {
  assert.equal(percentOf(0, 10), 0)
  assert.equal(percentOf(5, 10), 50)
  assert.equal(percentOf(10, 10), 100)
  // Done 与 Total 来自不同时刻的事件（中止/跳过会让总数在途收窄），
  // 瞬时 Done > Total 时原先直接把 120 当宽度 → 填充条顶出轨道。
  assert.equal(percentOf(12, 10), 100)
  assert.equal(percentOf(-3, 10), 0)
})

test('percentOf 对缺字段与非法值回 0，绝不产出 NaN', () => {
  for (const [d, t] of [[0, 0], [5, 0], [NaN, 10], [5, NaN], [5, undefined], [undefined, 10]] as any[]) {
    const v = percentOf(d, t)
    assert.ok(Number.isFinite(v), `percentOf(${d}, ${t}) 应为有限数，实际 ${v}`)
    assert.equal(v, 0)
  }
})
