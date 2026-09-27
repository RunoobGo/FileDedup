// 按钮级「禁用态与它的解释同源」（M233 / M292 / M296，2026-09-27 实施批）。
//
// 这三条登记的是同一个形状：`store.busy` 由**三个原因**驱动（`opsRunning` / `scanning` /
// `histLoading`，见 `stores/scan.ts` 的 `busyReason()`），而视图里若干按钮的 `:title` 只问其中
// **一个**原因，甚至自带一套**与 `busyReason()` 顺序相反**的优先级（M292 的真机读数：
// 扫描那一档在两处给出两个不同句子）。⇒ 按钮是灰的，悬浮文案却写着"恢复 3 项文件"，
// 读起来像"能点、点了会回撤"。
//
// 为什么这条要在 node 层做**结构解析**，而不是 `test-frontend-logic.sh` 的一枚 grep 锚：
// M296 顺带记档了那些锚的结构性漏网——它们只问"某个标识符在不在文件里"，
// **没有按钮级作用域**，所以同一个文件里"这个按钮的 `:disabled` 与 `:title` 用的是两把尺子"
// 这种形状根本不在射程内（04 §4.3 也记着组件渲染面整个缺席）。这里逐按钮配对，才钉得住。
//
// ★ `.vue` 打不进 `node --test`（M116/M118 同族限制），所以本文件读**源文件**再解析——
//   先例是 `selection-wording.test.ts` 与 `records-keepsource.test.ts`。
//   断言一律用 `assert.ok(re.test(src), msg)`：`assert.match` 失败时会把整个 8 KB SFC 打进报告。

import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readdirSync, readFileSync } from 'node:fs'

type Tag = { name: string; attrs: Map<string, string> }

// 扫出某个起始标签的完整属性表。
//
// ★ 不能拿 `/<button[^>]*>/` 一步到位：属性值里合法地出现 `>`（例如 `:title="'a > b'"`），
//   非贪婪匹配会在一句话中间把标签截断，于是"这个按钮没有 :title"这种假红。
//   这里按字符走并跟踪引号状态，只在**引号外**的 `>` 收尾。
function findTags(src: string, tag: string): Tag[] {
  const out: Tag[] = []
  const needle = `<${tag}`
  let from = 0
  for (;;) {
    const start = src.indexOf(needle, from)
    if (start < 0) return out
    from = start + needle.length
    // 必须紧跟空白或 `>`，否则 `<buttonbar` 也算命中。
    const next = src[from]
    if (next && !/[\s/>]/.test(next)) continue
    let i = from
    let quote = ''
    for (; i < src.length; i++) {
      const c = src[i]
      if (quote) {
        if (c === quote) quote = ''
        continue
      }
      if (c === '"' || c === "'") { quote = c; continue }
      if (c === '>') break
    }
    const body = src.slice(from, i)
    const attrs = new Map<string, string>()
    // 属性名= "值" | {值} | 裸名；值里的换行保留（多行属性在本仓很常见）。
    const re = /(:[\w.-]+|[\w-]+)(?:\s*=\s*(?:"([\s\S]*?)"|'([\s\S]*?)'|\{([^}]*)\}))?/g
    let m: RegExpExecArray | null
    while ((m = re.exec(body)) !== null) {
      const name = m[1]
      const val = m[2] ?? m[3] ?? m[4] ?? ''
      if (!attrs.has(name)) attrs.set(name, val)
    }
    out.push({ name: tag, attrs })
  }
}

const RESULT_VIEW = 'src/views/ResultView.vue'
const RECORDS_VIEW = 'src/views/RecordsView.vue'
const SCAN_VIEW = 'src/views/ScanView.vue'

function viewSrc(file: string): string {
  return readFileSync(new URL('../' + file, import.meta.url), 'utf8')
}

// 视图里"因忙而灰"的判据只有这三个名字（`opDisabled` 在 ResultView 里含 `store.busy`）。
const BUSY_JUDGES = ['store.busy', 'histBusy', 'opDisabled']
// 忙理由的唯一文案出口：`busyTip`（由 `busyReason()` 派生）与在它外面再包一层的 `opDisabledTip`。
const SAME_SOURCE = ['busyTip', 'opDisabledTip']

function mentionsBusy(expr: string): boolean {
  return BUSY_JUDGES.some((j) => expr.includes(j))
}

// ★ 下面刻意**不用 `for (const v of VIEWS) test(…)` 生成用例**：门禁第 12 行（version-sync 的
//   B4 组）的前端通道是 `grep -h '^test(' frontend/tests/*.test.ts`，它的前提是"一行字面量 ≡
//   一项被执行的用例"。循环把 6 项压成 2 行，静态通道当场和执行计数分叉（本批实测 129 vs 135
//   ⇒ 那一格红）。逐个摆出来多写 4 行，换来那条锚仍是纯 grep、不依赖 node——CI 三条腿同形。
//   这条取舍写在这里，是为了下一位读者别再"顺手改回循环"。

// 判据 (a)：`:disabled` 问的是"忙不忙"的按钮，标题必须问同一家的忙理由。
//
// ★ 静态 `title="…"` 一并纳入：M296 的缺陷面是"灰着却说不清为什么灰"，而一句写死的
//   「按此配置重新扫描」挂在灰按钮上同样说不清——它讲的是这个按钮**干什么**，
//   不是**为什么不能点**。只钉 `:title` 会留下这条后门（本批实测多抓出两格）。
//
// 为什么允许"根本没有 title"这一格先不纳入：本条钉的是**已经写了一句解释、但那句解释
// 是假解释**的形状（M292/M296 的实例都带着 title）。给每个按钮补悬浮是另一件事。
function checkTitlesShareBusySource(file: string) {
  const bad: string[] = []
  for (const btn of findTags(viewSrc(file), 'button')) {
    const disabled = btn.attrs.get(':disabled') ?? ''
    if (!disabled || !mentionsBusy(disabled)) continue
    const title = btn.attrs.get(':title') ?? btn.attrs.get('title')
    if (title === undefined) continue
    if (SAME_SOURCE.some((s) => title.includes(s))) continue
    bad.push(`:disabled="${disabled.trim()}" 配 title="${title.trim()}"`)
  }
  assert.equal(
    bad.join('\n'),
    '',
    '按钮的 :disabled 问的是三因 busy，标题却没问 busyReason 的那一家 ⇒ 灰着却说不清为什么灰' +
      `（M233/M292/M296）：\n${bad.join('\n')}`,
  )
}

test(`${RESULT_VIEW}：因忙而灰的按钮，悬浮文案必须取自同一家的忙理由`, () =>
  checkTitlesShareBusySource(RESULT_VIEW))
test(`${RECORDS_VIEW}：因忙而灰的按钮，悬浮文案必须取自同一家的忙理由`, () =>
  checkTitlesShareBusySource(RECORDS_VIEW))
test(`${SCAN_VIEW}：因忙而灰的按钮，悬浮文案必须取自同一家的忙理由`, () =>
  checkTitlesShareBusySource(SCAN_VIEW))

// 判据 (b)：视图里不得再自带一套忙理由的优先级。M292 的真机读数正是这两套顺序**相反**。
function checkNoSecondBusyLadder(file: string) {
  const bad: string[] = []
  for (const btn of findTags(viewSrc(file), 'button')) {
    const title = btn.attrs.get(':title')
    if (!title) continue
    if (/store\.(scanning|opsRunning|histLoading)\s*\?/.test(title)) {
      bad.push(title.trim())
    }
  }
  assert.equal(
    bad.join('\n'),
    '',
    '忙理由的优先级只在 stores/scan.ts 的 busyReason() 里定义一次；视图自拼三元式就是第二套' +
      `口径（M292 读到两套顺序相反的真机读数）：\n${bad.join('\n')}`,
  )
}

test(`${RESULT_VIEW}：:title 里不得自拼 scanning / opsRunning 的优先级（busyReason 已收口）`, () =>
  checkNoSecondBusyLadder(RESULT_VIEW))
test(`${RECORDS_VIEW}：:title 里不得自拼 scanning / opsRunning 的优先级（busyReason 已收口）`, () =>
  checkNoSecondBusyLadder(RECORDS_VIEW))
test(`${SCAN_VIEW}：:title 里不得自拼 scanning / opsRunning 的优先级（busyReason 已收口）`, () =>
  checkNoSecondBusyLadder(SCAN_VIEW))

// 判据 (c)：记录页那条**可见**的忙提示同样得问一家。
//
// 为什么单独一条、且钉的是可见文本而不是 title：M286 立的取向是"把'为什么是灰的'摆成
// 可见文本"（title 由 OS 绘制，M293 证过合成 hover 唤不起——能证"挂在元素上"，
// 不能证"用户看得见"）。改前这行写的是 `v-if="tab === 'ops' && store.opsRunning"`
// 配一句写死的「回撤执行中…」⇒ 扫描中/历史载入中按钮同样全灰，页面上一句解释都没有。
test('RecordsView：可见的忙提示由 busyTip 供文案，不再单判 opsRunning 写死一句', () => {
  const src = viewSrc(RECORDS_VIEW)
  assert.ok(
    /class="undo-hint"[^>]*>\s*\{\{\s*store\.busyTip\s*\}\}/.test(src) ||
      /\{\{\s*store\.busyTip\s*\}\}[^<]*<\/span>/.test(src),
    '记录页的可见忙提示须直接渲染 store.busyTip（三因各有一句专属文案）',
  )
  assert.ok(
    !/v-if="[^"]*store\.opsRunning[^"]*"[^>]*class="undo-hint"/.test(src) &&
      !/class="undo-hint"[^>]*回撤执行中…/.test(src),
    '不得再出现"单判 opsRunning + 写死一句回撤执行中"的形状（M296：另两因灰着却无解释）',
  )
  assert.ok(
    !/:title="store\.opsRunning\s\?\s*'操作执行中'/.test(src),
    '「操作执行中」这一句的判据面须并入 busyTip（M296 的 :288/:325 两处实例）',
  )
})

// 判据 (d)：上面 (a)(b) 的**扫描面**自证——"因忙而灰"的判据只许出现在这三枚视图里。
//
// 为什么需要这一条：(a)(b) 都只遍历 RESULT/RECORDS/SCAN 三个文件，所以"第四处按钮
// 也拿 store.busy 灰掉了、但悬浮写的是别的句子"这种形状**不在射程内**，而且本文件
// 一声不响（与 event-channel-names.test.ts 的 (a)(b) 同一族：宁可红在前提）。
// 现读读数：src 下 15 枚 .vue 里，只有这三枚视图命中 BUSY_JUDGES，10 枚组件与 App.vue
// 与 SettingsView 均为 0 处（2026-09-27 本批 grep）⇒ 今天这前提是成立的。
test('busy 判据的分布面 = 这三枚视图（第四处出现就得扩扫描面，别让本门静默失明）', () => {
  const vueFiles = (readdirSync(new URL('../src/', import.meta.url), { recursive: true }) as string[])
    .filter((f) => f.endsWith('.vue'))
    .map((f) => f.replace(/\\/g, '/'))
    .sort()
  const inScope = [RESULT_VIEW, RECORDS_VIEW, SCAN_VIEW]
  const strays = vueFiles
    .filter((f) => !inScope.includes(`src/${f}`))
    .filter((f) => BUSY_JUDGES.some((j) => readFileSync(new URL(`../src/${f}`, import.meta.url), 'utf8').includes(j)))
  assert.ok(
    vueFiles.length >= 3,
    `src 下只读到 ${vueFiles.length} 枚 .vue ⇒ 扫描面本身塌了，(a)(b) 的绿不可信`,
  )
  assert.equal(
    strays.join('\n'),
    '',
    '出现了本文件没覆盖的"因忙而灰"判据 ⇒ 把它纳进上面的三视图名单（并补两条判据），' +
      '而不是让这一格继续只查三个文件：\n' + strays.join('\n'),
  )
})
