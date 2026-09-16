// P2-7：轻量 Markdown 渲染器（无第依赖、无 v-html）。
//
// 为什么不用现成库、也不用 v-html：
//   1. 项目零运行时依赖（仅 vue + pinia），为预览面板引入 marked/markdown-it 不划算；
//   2. 预览内容来自**用户磁盘上的任意文件**，属于不可信输入，用 v-html 注入渲染结果
//      就必须自己保证净化正确 —— 与其如此，不如把 Markdown 解析成结构化数据，
//      交给 Vue 模板渲染。Vue 的插值天然转义，从根上没有 XSS 面。
//      唯一的逃逸点是链接的 href，因此对协议做了白名单。
//
// 支持范围（覆盖笔记/文档的常见写法）：ATX 标题、围栏代码、引用、有序/无序列表、
// 分隔线、段落；行内支持 `code`、**粗体**、*斜体*、~~删除线~~、[链接](url)。
// 故意不支持表格/HTML/嵌套行内 —— 保持"轻量"，超出范围的语法按纯文本原样显示。

export type Block =
  | { t: 'h'; level: number; text: string }
  | { t: 'p'; text: string }
  | { t: 'ul'; items: string[] }
  | { t: 'ol'; items: string[] }
  | { t: 'quote'; text: string }
  | { t: 'code'; lang: string; text: string }
  | { t: 'hr' }

export interface Seg {
  k: 'text' | 'b' | 'i' | 'code' | 'strike' | 'link'
  text: string
  href?: string
}

/** 链接协议白名单：拒绝 javascript: / data: / vbscript: 等可执行协议 */
export function safeHref(raw: string): string | null {
  const url = raw.trim()
  if (!url) return null
  // 无协议的相对/锚点链接放行
  if (/^[#/?]/.test(url) || /^[\w.\-]+\//.test(url)) return url
  if (/^(https?:|mailto:)/i.test(url)) return url
  return null
}

/** 行内解析：单趟、不嵌套（再次强调：轻量，不追求 CommonMark 完备） */
export function parseInline(src: string): Seg[] {
  const out: Seg[] = []
  // 依次匹配：行内代码 > 链接 > 粗体 > 斜体 > 删除线
  const re = /(`[^`]+`)|(\[[^\]]*\]\([^)\s]+\))|(\*\*[^*]+\*\*)|(\*[^*]+\*)|(~~[^~]+~~)/g
  let last = 0
  let m: RegExpExecArray | null
  while ((m = re.exec(src))) {
    if (m.index > last) out.push({ k: 'text', text: src.slice(last, m.index) })
    const tok = m[0]
    if (tok.startsWith('`')) {
      out.push({ k: 'code', text: tok.slice(1, -1) })
    } else if (tok.startsWith('[')) {
      const mm = /^\[([^\]]*)\]\(([^)\s]+)\)$/.exec(tok)
      if (mm) {
        const href = safeHref(mm[2])
        if (href) out.push({ k: 'link', text: mm[1], href })
        else out.push({ k: 'text', text: mm[1] }) // 协议不被允许时退化为纯文本，不生成可点链接
      } else {
        out.push({ k: 'text', text: tok })
      }
    } else if (tok.startsWith('**')) {
      out.push({ k: 'b', text: tok.slice(2, -2) })
    } else if (tok.startsWith('~~')) {
      out.push({ k: 'strike', text: tok.slice(2, -2) })
    } else {
      out.push({ k: 'i', text: tok.slice(1, -1) })
    }
    last = m.index + tok.length
  }
  if (last < src.length) out.push({ k: 'text', text: src.slice(last) })
  return out
}

const HR = /^\s{0,3}([-*_])(?:\s*\1){2,}\s*$/
const H = /^\s{0,3}(#{1,6})\s+(.*)$/
const UL = /^\s{0,3}[-*+]\s+(.*)$/
const OL = /^\s{0,3}\d+[.)]\s+(.*)$/
const QUOTE = /^\s{0,3}>\s?(.*)$/
const FENCE = /^\s{0,3}(?:```|~~~)\s*([\w+-]*)\s*$/

/** 块级解析。输入为已按 \n 切分的行数组，输出结构化块序列。 */
export function parseMarkdown(src: string): Block[] {
  const lines = src.replace(/\r\n?/g, '\n').split('\n')
  const blocks: Block[] = []
  let i = 0
  let para: string[] = []
  const flushPara = () => {
    if (para.length) {
      blocks.push({ t: 'p', text: para.join(' ').trim() })
      para = []
    }
  }

  while (i < lines.length) {
    const line = lines[i]

    // 围栏代码块：整块原样收集，内部不再解析
    const fence = FENCE.exec(line)
    if (fence) {
      flushPara()
      const lang = fence[1] || ''
      const buf: string[] = []
      i++
      while (i < lines.length && !FENCE.test(lines[i])) {
        buf.push(lines[i])
        i++
      }
      i++ // 跳过结束围栏（缺失时视为到文件末尾）
      blocks.push({ t: 'code', lang, text: buf.join('\n') })
      continue
    }

    if (!line.trim()) {
      flushPara()
      i++
      continue
    }
    if (HR.test(line)) {
      flushPara()
      blocks.push({ t: 'hr' })
      i++
      continue
    }
    const h = H.exec(line)
    if (h) {
      flushPara()
      blocks.push({ t: 'h', level: h[1].length, text: h[2].trim() })
      i++
      continue
    }
    if (QUOTE.test(line)) {
      flushPara()
      const buf: string[] = []
      while (i < lines.length && QUOTE.test(lines[i])) {
        buf.push(QUOTE.exec(lines[i])![1])
        i++
      }
      blocks.push({ t: 'quote', text: buf.join(' ').trim() })
      continue
    }
    if (UL.test(line) || OL.test(line)) {
      flushPara()
      const ordered = OL.test(line)
      const re = ordered ? OL : UL
      const items: string[] = []
      while (i < lines.length && re.test(lines[i])) {
        items.push(re.exec(lines[i])![1].trim())
        i++
        // 缩进续行并入上一项，保住"软换行属于同一列表项"的直觉
        while (i < lines.length && /^\s{2,}\S/.test(lines[i]) && !re.test(lines[i])) {
          items[items.length - 1] += ' ' + lines[i].trim()
          i++
        }
      }
      blocks.push(ordered ? { t: 'ol', items } : { t: 'ul', items })
      continue
    }

    para.push(line.trim())
    i++
  }
  flushPara()
  return blocks
}

/** 该扩展名是否按 Markdown 处理 */
export function isMarkdownExt(ext: string): boolean {
  return /^\.(md|markdown|mdown|mkd)$/i.test(ext)
}
