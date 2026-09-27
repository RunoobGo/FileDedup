// 事件通道名比对门（M303，2026-09-27 实施批）。
//
// 这一族缺陷的形状：后端 `a.emit(ctx, "ops:filtered", …)` 改名叫 `ops:narrowed`，
// 而前端 `bind('ops:filtered', …)` 没跟着改 ⇒ **两边各自全绿**：
// go test 全绿（发事件不需要有人听），vue-tsc 全绿（`bind(name: string, …)` 收任何字符串），
// vite build 全绿，真机上那一格数据永远不更新——界面停在旧值上，不报错、不提示。
// 这是"静默丢弃"方向。反方向（前端监听一个后端从没发过的名字）同样静默：
// 回调永远不触发，写它的人以为接上了。
//
// 为什么方法名那一半不需要这里重做：`backend()` 的返回类型是 `BackendAPI`，
// 前端调一个没声明的方法是编译错误；"声明了但 Go 侧没有"由根包
// `TestBackendAPIMatchesGoExportedMethods`（M46/G5，双向零豁免）钉住。
// **事件名两边都是裸字符串**，没有任何类型层牵住它们，所以只能在这里比。
//
// ★ 与 `busy-title-source.test.ts` 同一族限制：`.vue` 打不进 `node --test`（M116/M118），
//   所以本文件读**源文件**再正则提取，断言在两个集合之间做，不在单侧做。
//   断言一律 `assert.equal(坏清单.join('\n'), '', msg)`：失败时只打出生问题的调用点，
//   不把 30 个文件的内容打进报告。
//
// ★ 下面刻意**不用 `for (const …) test(…)` 生成用例**：B4 真值通道是
//   `grep -h '^test(' frontend/tests/*.test.ts`，其前提是"一行 col-0 `test(` ≡ 一格被执行的用例"，
//   循环生成的用例满足不了这个 1:1（本批实测 129 vs 135 那一格红就是这个形状）。
//   这条取舍写在这里，是为了下一位读者别再"顺手改回循环"。
//
// ★ 比"集合相等"更容易漏的是**扫描面自己塌了**：如果哪天有人绕过 `onEvent` 直接调
//   `window.runtime.EventsOn`，或把事件名改成变量拼出来的，本文件会读到"两侧都空/都少"，
//   然后**照样绿**。所以判据 (a)(b) 是 fail-closed 的前置门：先证明每一处调用点都能被
//   本文件看见，再比集合。宁可红在前提，不可绿在盲区。

import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readdirSync, readFileSync } from 'node:fs'

const ROOT = new URL('../../', import.meta.url)
const FE_SRC = new URL('../src/', import.meta.url)

type Site = { name: string; where: string }

// ---- 后端事件面 -------------------------------------------------------------
//
// 扫描面 = 仓库根的**非测试** `.go`（生产发射点全在 `app.go` / `app_single_instance.go`；
// `internal/*` 拿不到 `a.emit`，测试文件里的 `rec.emit` 是桩不是生产面）。
const GO_FILES = readdirSync(ROOT)
  .filter((f) => f.endsWith('.go') && !f.endsWith('_test.go'))
  .sort()

function readGo(file: string): string {
  return readFileSync(new URL(file, ROOT), 'utf8')
}

/** 生产发射点：`.emit(` 调用（不含 `emit func(...)` 字段声明与 `a.emit = ` 赋值）。 */
function goEmitCallCount(src: string): number {
  return [...src.matchAll(/\.emit\(/g)].length
}

/** 能被他看见的发射点：第二参数是字符串字面量。 */
function goEmitSites(file: string): Site[] {
  const src = readGo(file)
  const out: Site[] = []
  for (const m of src.matchAll(/\.emit\(\s*[^,]+,\s*"([^"]+)"/g)) {
    out.push({ name: m[1], where: `${file}:${lineNo(src, m.index ?? 0)}` })
  }
  return out
}

/** 看不见的事件面：`.emit(ctx, 变量, …)`——名字要到运行时才知道，比对门射程外。 */
function goEmitBlindSpots(file: string): string[] {
  const src = readGo(file)
  const total = goEmitCallCount(src)
  const literal = goEmitSites(file).length
  const bad: string[] = []
  if (total !== literal) bad.push(`${file}：${total} 处 .emit( 调用，只有 ${literal} 处能解析出字面量事件名`)
  return bad
}

// ---- 前端订阅面 -------------------------------------------------------------

// 递归收 src 下的 .ts / .vue（含 views / stores / components / utils）。
// 分段在 Windows 上是反斜杠，先归一成 `/`：URL 拼接、显示、下面的外壳名单比较都用同一形状。
const FE_FILES = (readdirSync(FE_SRC, { recursive: true }) as string[])
  .filter((f) => /\.(ts|vue)$/.test(f) && !f.endsWith('.d.ts'))
  .map(show)
  .sort()

function readFe(file: string): string {
  return readFileSync(new URL(file, FE_SRC), 'utf8')
}

/** 订阅点：`bind('x', …)`（store 内的收口）与 `onEvent('x', …)`（外壳本身）。 */
function feBindSites(file: string): Site[] {
  const src = readFe(file)
  const out: Site[] = []
  for (const m of src.matchAll(/(?:\bbind|onEvent)\(\s*'([^']+)'/g)) {
    out.push({ name: m[1], where: `src/${show(file)}:${lineNo(src, m.index ?? 0)}` })
  }
  return out
}

// 允许"事件名是形参"的位置**只有两处**，且各自恰好一次——它们是外壳本身：
// `wails.ts` 的 `export function onEvent(name: string, …)` 声明，
// 和 `stores/scan.ts` 里 `bind()` 内部那句 `onEvent(name, cb)` 转发。
// 外壳必须能转发，否则它不是外壳；但**第三处**转发意味着有人在另建一套漏斗，
// 那正是要拦的形状，所以这里按"每个外壳文件恰好一次"卡死，而不是宽松放过。
const FUNNEL_FILES = ['wails.ts', 'stores/scan.ts']

/** 旁路与盲点：绕开 onEvent 直接 EventsOn，或订阅名不是字面量。 */
function feBindBlindSpots(file: string): string[] {
  const src = readFe(file)
  const bad: string[] = []
  // 真实订阅只有 `onEvent` 一个出口（wails.ts:509）；旁路调用本文件看不见。
  const direct = [...src.matchAll(/EventsOn\(/g)].length
  const viaWrapper = /export function onEvent\(/.test(src) ? 1 : 0
  if (direct !== viaWrapper) {
    bad.push(`src/${show(file)}：${direct} 处 EventsOn( 调用，本文件只认 onEvent 外壳里的 ${viaWrapper} 处`)
  }
  // 名字写成变量 ⇒ 静态比对看不见那一条。除上面两处外壳外一律红。
  const funnel = FUNNEL_FILES.includes(show(file))
  const forwards: number[] = []
  for (const m of src.matchAll(/(?:\bbind|onEvent)\(\s*([^'"\s)][^,\n]*)/g)) {
    const arg = m[1].trim()
    if (funnel && arg.startsWith('name')) {
      forwards.push(lineNo(src, m.index ?? 0))
      continue
    }
    bad.push(`src/${show(file)}:${lineNo(src, m.index ?? 0)}：订阅名不是字面量（${arg.slice(0, 40)}）`)
  }
  if (funnel && forwards.length !== 1) {
    bad.push(`src/${show(file)}：外壳转发句柄有 ${forwards.length} 处（: ${forwards.join(', ')}），应为恰好 1 处——多一处就是在另建一套订阅漏斗`)
  }
  return bad
}

// readdirSync 在 Windows 上给出反斜杠分段，仅用于显示与比较（读取仍用原样 URL）。
function show(file: string): string {
  return file.replace(/\\/g, '/')
}

function lineNo(src: string, index: number): number {
  let n = 1
  for (let i = 0; i < index; i++) if (src[i] === '\n') n++
  return n
}

function uniq(list: Site[]): Site[] {
  const seen = new Map<string, Site>()
  for (const s of list) if (!seen.has(s.name)) seen.set(s.name, s)
  return [...seen.values()].sort((a, b) => a.name.localeCompare(b.name))
}

const GO_SITES = GO_FILES.flatMap(goEmitSites)
const FE_SITES = FE_FILES.flatMap(feBindSites)

// ---- 判据 (a)：扫描面自证 ---------------------------------------------------

test('事件比对门 (a)：后端每个 .emit( 调用的事件名都是字面量，且两侧扫描面非空', () => {
  assert.ok(
    GO_FILES.length >= 1 && GO_SITES.length >= 1,
    `后端扫描面为空（GO_FILES=${GO_FILES.length}、命中发射点=${GO_SITES.length}）⇒ ` +
      '比对门会在"两边都没事件"上读到绿，这是假绿不是通过（AS-K2：SKIP/空转永不读作通过）',
  )
  const blind = GO_FILES.flatMap(goEmitBlindSpots)
  assert.equal(
    blind.join('\n'),
    '',
    '存在本文件看不见的事件发射点（事件名是变量拼出来的）⇒ 集合比对对它失明，' +
      '后面两条判据的绿不可信；先把名字收回复面字面量，或在此显式登记为豁免：\n' + blind.join('\n'),
  )
})

test('事件比对门 (b)：前端订阅只经 onEvent / bind 两个复面出口', () => {
  assert.ok(
    FE_FILES.length >= 1 && FE_SITES.length >= 1,
    `前端扫描面为空（FE_FILES=${FE_FILES.length}、命中订阅点=${FE_SITES.length}）⇒ 同 (a)，假绿`,
  )
  const blind = FE_FILES.flatMap(feBindBlindSpots)
  assert.equal(
    blind.join('\n'),
    '',
    '存在本文件看不见的订阅点（绕过 onEvent 外壳直调 EventsOn，或事件名写成变量）' +
      '⇒ 静默丢弃方向从此处漏出去：\n' + blind.join('\n'),
  )
})

// ---- 判据 (c)：正向（后端发了，前端必须听）---------------------------------

test('事件比对门 (c)：后端每个发射点都有人听（静默丢弃方向）', () => {
  const heard = new Set(FE_SITES.map((s) => s.name))
  const orphan = uniq(GO_SITES.filter((s) => !heard.has(s.name)))
  assert.equal(
    orphan.length,
    0,
    '后端在发、前端没听 ⇒ 界面停在新值之前，且不报错（go test / vue-tsc / vite build 三道门禁全绿）：\n' +
      orphan.map((s) => `  ${s.name}  ← ${s.where}`).join('\n') +
      '\n要么在 stores/scan.ts 的 bindEvents() 里补订阅，要么把这次发射一并删掉。',
  )
})

// ---- 判据 (d)：反向（前端听了，后端必须发）---------------------------------

test('事件比对门 (d)：前端每个订阅名后端都会发（死监听方向）', () => {
  const sent = new Set(GO_SITES.map((s) => s.name))
  const dead = uniq(FE_SITES.filter((s) => !sent.has(s.name)))
  assert.equal(
    dead.length,
    0,
    '前端在听、后端从没发过 ⇒ 回调永不触发，写它的人会以为这一格接上了（M4/M94 同族：' +
      '名字两边都是裸字符串，没有类型层牵住）：\n' +
      dead.map((s) => `  ${s.name}  ← ${s.where}`).join('\n'),
  )
})
