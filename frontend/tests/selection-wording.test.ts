// R3-1（2026-09-23 第四轮全仓审查，设计段 §30.7 ①）：组卡片上"勾了一个冗余项"这件事
// 到底承诺了什么。
//
// 缺陷原形：GroupCard 在同一张卡片里对同一件事说两种话——组级全选（:37/:43）写"待清理项"，
// 单项勾选（:61/:68/:75）写"待删除项 / 标记为删除 / 勾选后将被删除"。后者是假承诺：
// 勾选之后要走五颗按钮里的一颗（回收站 / 移动 / 硬链接 / 软链接 / 永久删除），
// **只有 delete 是删除**；trash 是改名进回收站（M22 裁定它不得说"释放/删除"）、
// hardlink 与 symlink 保留项仍可访问、move 只是离开原位置，处理策略还会再收窄一次实际范围。
// 单项那条尤其重：`标记为删除 ${f.name}` 是 aria-label，屏幕阅读器用户只有这一句。
//
// ★ 本文件的"改前必红"由变异提供（与 op-failed-label.test.ts 同一条说明）：
//   `selectionWording` 是新符号，改前 import 它只会得到"没有这个导出"，
//   那是红在错误的格子上，不能当修前证据。修前的真读数在 §30.7 的接线锚那一步。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

import { selectionWording } from '../src/utils/opdisplay'

// 读组件源码而不是 import 它：`.vue` 在 node 的裸执行环境里加载不了（要 vite 的 SFC 编译），
// 而这里要问的只是"组级那两句此刻写的是什么"。手法先例：M30 比对器读 TS 源文件、
// Go 侧 P-1 钉读 filter.go 源文件。
const groupCardSrc = readFileSync(new URL('../src/components/GroupCard.vue', import.meta.url), 'utf8')
const recordsSrc = readFileSync(new URL('../src/views/RecordsView.vue', import.meta.url), 'utf8')

const w = selectionWording()
const all = [w.checkTitle, w.ariaVerb, w.redundantTitle]

test('三句里都不许出现"删除"——五种操作只有 delete 是删除', () => {
  for (const s of all) {
    assert.doesNotMatch(s, /删除|销毁|不可恢复/, `实测 ${s}：勾选阶段还谈不上删除`)
  }
})

test('单项勾选与组级全选同词（改前是一张卡片两种说法）', () => {
  // 组级全选那两句住在组件模板里，钉它们是因为改前它自己就与单项不一致，
  // 而"退回去只改单项那三句"照样能让上面几条全绿。
  assert.match(groupCardSrc, /该组待清理项/, 'GroupCard 组级全选的措辞被改走了，同词这条前提没了')
  assert.match(groupCardSrc, /sel\.checkTitle/, '单项 title 没在引用集中出口（被改回内联）')
  assert.match(groupCardSrc, /sel\.ariaVerb/, '单项 aria-label 没在引用集中出口（被改回内联）')
  assert.match(w.checkTitle, /待清理项/, `实测 ${w.checkTitle}`)
  assert.match(w.ariaVerb, /待清理/, `实测 ${w.ariaVerb}`)
})

test('冗余徽标只说"按所选操作处理"，不预设结果', () => {
  assert.match(w.redundantTitle, /按所选操作/)
  // 保留项身份在这三句之外（由 isKeep 分支另说），这里钉的是"冗余项"那一句不许写死后果。
  assert.doesNotMatch(w.redundantTitle, /将被|就会|一定会/, `实测 ${w.redundantTitle}`)
})

// ②③ 两条把"文案真伪"这层职责接过来（R3-1）。为什么放这里而不是接线锚：
// scripts/test-frontend-logic.sh 开头自己立的规矩是"只锚标识符、锚中文文案会误报"，
// 而 `RecordsView` 的空态与 `ScanView` 的禁用判据都是**一句人话**，没有专属标识符可锚
// （`m.undoable`、`store.histBusy` 本就在文件里，锚它们改前也绿 ⇒ 假锚）。
// 于是锚只钉"旧写法有没有回来"，句子本身对不对由本文件的正则说了算。
test('记录页空态不替所有记录打包票：可撤性指回逐条徽标', () => {
  const line = recordsSrc.match(/<p class="empty-desc">([^<]*留痕[^<]*)<\/p>/)
  assert.ok(line, '找不到空态那句含"留痕"的 <p class="empty-desc">（结构被改走了）')
  assert.match(line[1], /徽标/, `实测 ${line[1]}：可撤性的唯一出处是每条记录的徽标`)
  // 后端 `undoableFor`（app.go:1678）判 Windows 的 trash 不可应用内回撤、
  // 软链另有 undoSymlink ⇒ 任何"所有操作都能撤"式总述都是假承诺。
  assert.doesNotMatch(line[1], /支持对回收站|均可回撤|都能撤|全部支持/, `实测 ${line[1]}`)
})

test('扫描页历史横幅的恢复按钮问的是 histBusy 而不是 opsRunning', () => {
  const src = readFileSync(new URL('../src/views/ScanView.vue', import.meta.url), 'utf8')
  const btn = src.match(/<button[^>]*:disabled="([^"]+)"[^>]*>\s*恢复/)
  assert.ok(btn, '找不到"恢复"那颗按钮的 :disabled（模板改走了）')
  assert.equal(btn[1], 'store.histBusy', `实测 ${btn[1]}`)
})

