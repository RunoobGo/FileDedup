// M290/M297 实施批：记录页对软链接合并说了什么、给了什么出口。
//
// 与 `keepsource.test.ts` 的分工：那一只测**文案方本身**（纯逻辑），这一只测
// **视图有没有去问那一家**——`.vue` 打不进 node --test（M116/M118 同族限制），
// 手法是读源文件（先例：selection-wording.test.ts 读 GroupCard.vue、M30 比对器读 TS 源）。
//
// ★ 断言一律走 `assert.ok(re.test(src))` 而不是 `assert.match(src, re)`：后者把
//   整个 .vue 源码当 actual 打进失败输出（实测 8 KB 一屏），一条红读不出命题在哪。
// ★ 本文件的"修前必红"是**真的改前读数**，不靠变异：这些锚在改前的 RecordsView.vue
//   里一条都不存在，而「备份仍在，回撤不依赖链接是否有效」那句假承诺当时在场
//   （04 §6.50 一 W3-5 真机读数点名的正是它）。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const src = readFileSync(new URL('../src/views/RecordsView.vue', import.meta.url), 'utf8')

const has = (re: RegExp, why: string) => assert.ok(re.test(src), `${why}（锚：${re}）`)
const lacks = (re: RegExp, why: string) => assert.ok(!re.test(src), `${why}（被禁写法：${re}）`)

test('软链接行的假承诺已移出视图', () => {
  lacks(/备份仍在，回撤不依赖链接是否有效/, '这句是真机证明走不到的承诺（M290）：合并成功即删备份')
  lacks(/如需恢复成独立文件，点右侧「回撤」即可/, '同一句的后半段单独残留也算没改净')
})

test('悬空行与「数据在保留源」都问那一家文案（视图不得内联整句）', () => {
  has(/danglingKeepTitle\(/, '悬空行没在引用集中出口')
  has(/KEEP_SOURCE_LABEL/, '可见文案没取自集中出口')
  has(/keepSourceTitle\(/, '徽标说明没取自集中出口')
})

test('「回撤」出口由判据函数门控，而不是视图自己比 kind（M79 同一纪律）', () => {
  has(/offersInAppUndo\(/, '视图没问集中判据')
  lacks(/kind === 'symlink'/, '判据被抄回视图了：软链接这一条只许问 offersInAppUndo')
})

test('「直达保留原目录」真的接到后端绑定上，且失败可见', () => {
  has(/api\.revealKeepSource\(/, '按钮没调用后端 RevealKeepSource')
  has(/revealKeepTitle\(/, '按钮的悬浮说明没取自集中出口')
  // 静默的直达按钮就是 M83 那一族"点了没反应"的镜像，catch 必须在调用点在场。
  has(/revealKeepSource\([^)]*\)\s*\.catch/, '直达失败没挂 catch')
})
