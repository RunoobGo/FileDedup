// P2-7 / 拟 M385（2026-10-01 第九轮）：Markdown 预览的链接协议白名单**一条判据都没有**。
//
// 改前的覆盖取证（报告 §P2-7）：`grep -rln markdown frontend/tests` 与
// `grep -rn markdown scripts/*.sh` 均零命中 ⇒ 这枚渲染器有真实的渲染路径
// （`PreviewPanel.vue` 消费 parseInline 的 link 段），却没有任何一格钉住"什么协议能变成可点链接"。
// 危害面不是空谈：预览内容来自**用户磁盘上的任意文件**（不可信输入），
// 而 safeHref 是这条链上唯一的净化点——文件里写 `[点我](javascript:...)` 就该退化成纯文本。
//
// ★ 本文件刻意只测 `safeHref` / `parseInline` 这两枚纯函数：markdown.ts 不依赖 vue/pinia，
//   所以能直接进 node（这与 pageload 那格"store 也能进"是两条不同的可达性，各记各的）。
//
// ★ 有一条**按现行为钉住、不擅自收紧**的形状：协议相对 URL（`//evil.example/x`）会被
//   `/^[#/?]/` 当"相对链接"放行，浏览器按当前协议解析成外站绝对地址。要不要连它一起拒是
//   **产品取向**（改的是"链接能不能点"），所以这里钉成断言并登记为**待裁 M387**（设计段 §五 D2）。
//   ★ 读法提醒：这一格绿 ≠ 已裁定，它绿的是"今天确实放行"。
import { test } from 'node:test'
import assert from 'node:assert/strict'

import { safeHref, parseInline } from '../src/utils/markdown'

// ---------- P-59：可执行协议一律拒（逐条独立成格，撞号时能一眼看出是哪条协议漏了） ----------

test('javascript: 协议被拒（含大小写混写与前置空白）', () => {
  assert.equal(safeHref('javascript:alert(1)'), null)
  assert.equal(safeHref('JaVaScRiPt:alert(1)'), null)
  assert.equal(safeHref('   javascript:alert(document.cookie)'), null)
  assert.equal(safeHref('javascript:alert(1)//x'), null, '带路径后缀的写法也一样拒')
})

test('data: 协议被拒（base64 与非 base64 两种形状）', () => {
  assert.equal(safeHref('data:text/html,<script>alert(1)</script>'), null)
  assert.equal(safeHref('data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg=='), null)
})

test('vbscript: 协议被拒（IE 时代的老协议，白名单外就必须拒）', () => {
  assert.equal(safeHref('vbscript:msgbox(1)'), null)
  assert.equal(safeHref('VBScript:msgbox(1)'), null)
})

test('空串与纯空白：拒（不是"原样返回空链接"）', () => {
  assert.equal(safeHref(''), null)
  assert.equal(safeHref('   '), null)
  assert.equal(safeHref('\t\n'), null)
})

// ---------- 白名单内：原样放行，且返回的是 trim 之后的那个串 ----------

test('http / https / mailto 放行，且返回去空白后的原文', () => {
  assert.equal(safeHref('https://example.com/a?b=1#c'), 'https://example.com/a?b=1#c')
  assert.equal(safeHref('http://example.com'), 'http://example.com')
  assert.equal(safeHref('HTTPS://example.com'), 'HTTPS://example.com', '大小写不敏感地放行')
  assert.equal(safeHref('mailto:someone@example.com'), 'mailto:someone@example.com')
  assert.equal(safeHref('  https://example.com  '), 'https://example.com', '返回值必须是 trim 后的')
})

test('锚点与站内路径放行（README 里最常见的两种相对写法）', () => {
  assert.equal(safeHref('#section-2'), '#section-2')
  assert.equal(safeHref('/docs/a.md'), '/docs/a.md')
  assert.equal(safeHref('docs/a.md'), 'docs/a.md')
  assert.equal(safeHref('./a.md'), './a.md')
  assert.equal(safeHref('../a.md'), '../a.md')
})

// ---------- ★ 按现行为钉住的观察项（待裁 M387，不是已裁定） ----------

test('观察项：协议相对 URL 今天被放行——钉住现状，取向交裁定（M387）', () => {
  // `/^[#/?]/` 里那个 `/` 同时覆盖了"站内绝对路径"和"协议相对 URL"两种东西。
  // 后者在浏览器里解析成 `https://evil.example/x`（跟当前页协议），不是站内路径。
  // 本批**不改**：改它等于把"链接能不能点"这件事由代理替产品决定。
  assert.equal(safeHref('//evil.example/x'), '//evil.example/x',
    '★ 这一格绿只说明"今天确实放行"；若裁定改为拒绝，这条断言要跟着反过来')
  assert.equal(safeHref('   //evil.example/x'), '//evil.example/x')
})

// ---------- 逃逸点真正发生的地方：行内解析的降级路径 ----------

test('parseInline：可执行协议退化成纯文本，不生成可点链接段', () => {
  // ★ URL 里刻意不带圆括号：行内链接的正则是 `[^)\s]+`（markdown.ts:43），
  //   `javascript:alert(1)` 这种带 ')' 的串根本形不成链接 token，走的不是白名单那条腿。
  //   要打到 safeHref 返回 null 的**降级分支**（:56），得用能成形、但协议不被允许的 URL。
  const segs = parseInline('[点我](javascript:alert%281%29)')
  assert.deepEqual(segs.map((s) => s.k), ['text'], '恶意链接必须降级为 text 段，不能带 link 键')
  assert.equal(segs[0].text, '点我', '退化后保留链接文字（用户看得见内容，只是点不动）')
  assert.ok(!segs.some((s) => s.href !== undefined), '降级段不得携带 href')
})

test('parseInline：带括号的可执行协议今天连链接都不成（记现状，不是防线本身）', () => {
  // 这一格钉的是"现状无害"：`[点我](javascript:alert(1))` 被行内正则截断成两段纯文本，
  // 既没有 link 段也没有 href。★ 别把它读成白名单在起作用——它绿的原因是**语法层**先挡了一道，
  // 白名单那条腿由上面那格管。
  const segs = parseInline('[点我](javascript:alert(1))')
  assert.ok(!segs.some((s) => s.k === 'link'), '不该出现可点链接段')
  assert.ok(!segs.some((s) => s.href !== undefined), '不该携带任何 href')
})

test('parseInline：白名单内的链接正常生成 link 段并带上 href', () => {
  const segs = parseInline('看 [文档](https://example.com/a) 吧')
  const kinds = segs.map((s) => s.k)
  assert.ok(kinds.includes('link'), `白名单内的链接应生成 link 段，实得 ${JSON.stringify(kinds)}`)
  const link = segs.find((s) => s.k === 'link')!
  assert.equal(link.text, '文档')
  assert.equal(link.href, 'https://example.com/a')
})

test('parseInline：data: 与 vbscript: 走同一条降级腿（不靠特例）', () => {
  for (const bad of ['data:text/html;base64,PHN2Zy8+', 'vbscript:msgbox']) {
    const segs = parseInline(`[x](${bad})`)
    assert.deepEqual(segs.map((s) => s.k), ['text'], `${bad} 没有退化为纯文本`)
    assert.equal(segs[0].text, 'x')
  }
})
