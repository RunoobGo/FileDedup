// 展示层措辞（M15 起）：凡"某个字节数对用户意味着什么"的句子，集中在这里一份。
//
// 为什么集中：这类句子以前散在确认框、结果条、组卡片里各自拼字符串，
// 于是出现过"确认框说可释放 X、结果条说占用不变"的自相矛盾（M15）。
// 判据只有一份原则同样适用于文案。
//
// 口径来源是后端 `internal/ops/executor.go` 的 aggregate()：
//   hardlink → LinkedBytes（数据块变共享，用户看到的文件夹占用不变）
//   symlink  → SymlinkedBytes（磁盘上确实少一整份，但保留项仍可访问）
//   trash    → TrashedBytes（文件进了回收站：磁盘总量未减，清空后才释放）
//   其余     → Reclaimed（数据真的从磁盘消失：delete 与跨卷 move 出卷）
// 这里的措辞必须与那四个栏位一一对应，改任何一处都要看另一处。

import { humanBytes, formatCount } from './format'
import type { OpKind } from '../wails'

export interface ReclaimLine {
  lead: string
  bytes: string
  tail: string
}

// reclaimLine 说明这批字节的去向。lead/bytes/tail 三段是为了让调用方
// 仍能把数字加粗（整句返回字符串会丢掉强调，界面上一长串灰字更难读）。
export function reclaimLine(kind: OpKind, bytes: number): ReclaimLine {
  const n = humanBytes(bytes)
  switch (kind) {
    case 'hardlink':
      // 「可释放」在这里是假话：合并后两份路径都还在、都能打开，
      // 用户去资源管理器一看占用丝毫未变（结果条同批写的是"占用不变"）。
      return {
        lead: '共 ',
        bytes: n,
        tail: ' 数据将合并为硬链接（两份路径仍可访问，磁盘占用不变）',
      }
    case 'move':
      // 同卷移动是改名，磁盘总占用一点不变；只有跨卷才会在源盘腾出空间。
      // 预览阶段不知道每一项会不会跨卷（那是执行时的判定），所以两种可能都写出来。
      return {
        lead: '共 ',
        bytes: n,
        tail: ' 将离开原位置（同卷只是改名、磁盘总占用不变；跨卷才会在源盘腾出空间）',
      }
    case 'symlink':
      return {
        lead: '共 ',
        bytes: n,
        tail: ' 数据将只保留一份（冗余路径变成指向保留文件的软链接）',
      }
    case 'trash':
      // 「可释放」在这里同样是假话（M22，2026-09-21）：回收站里的数据仍在磁盘上
      // ——同卷只是一次改名，跨卷只是搬到另一个卷的回收站。要等用户清空回收站
      // 才真正腾出空间，所以措辞说"移入"而不说"释放"。
      return {
        lead: '共 ',
        bytes: n,
        tail: ' 将移入回收站（清空回收站后才真正释放空间）',
      }
    case 'delete':
      // 只有这一类是"数据真的从磁盘消失"，所以"可释放"这几个字归它专属。
      // 原先它住在 default 里（FE-2）：新增一类操作若忘了补 case，就会默默继承
      // 这句措辞——hardlink 那句"占用不变"翻了车正是同一族缺陷，措辞必须逐类显式。
      return { lead: '共 ', bytes: n, tail: ' 空间可释放' }
    default: {
      // FE-2 穷尽断言：OpKind 是联合类型，上面把五类都列全后 kind 在这里是 never。
      // 将来给 OpKind 加一类却不补分支，这一行就过不了 vue-tsc（门禁第 8 行），
      // 而不是悄悄走兜底措辞。下面的返回值仍保留，防的是"运行时收到未登记的 kind"
      // （后端加了类型、前端没跟上）——那种情况下宁可说一句不确定的话，也不能编一句。
      const _exhaustive: never = kind
      return { lead: '共 ', bytes: n, tail: ' 空间口径未登记（' + String(_exhaustive) + '）' }
    }
  }
}

// percentOf 把「已完成 / 总数」折成进度条宽度（0–100）。
//
// 为什么要钳：Done 与 Total 来自后端事件，不是同一时刻的快照——中止与跳过都会让
// 总数在途收窄，于是可能瞬时出现 Done > Total；原先那句 (Done / max(1, Total)) * 100
// 于是给出 >100 的宽度，填充条溢出自家轨道（视觉上"跑过头"，甚至顶破圆角）。
// 为什么要防 NaN：事件字段是外部输入，Total 缺省时原先的 max(1, …) 只挡住了除零，
// 没挡住 NaN——undefined / max(1, undefined) 是 NaN，浏览器对非法的 width 值
// 直接丢弃整条声明，进度条会停在 CSS 初始宽度上（看起来"卡在某个百分比不动"）。
export function percentOf(done: number, total: number): number {
  if (!Number.isFinite(done) || !Number.isFinite(total) || total <= 0) return 0
  return Math.max(0, Math.min(100, (done / total) * 100))
}

// opFailedLabel 结果横幅上那个"失败 N（查看）"按钮的措辞（M81）。
//
// 为什么需要它：全仓四个入口打开同一个失败抽屉，其中三处取 `store.failed.length`
// （页签徽标、统计条、空态按钮），只有结果横幅取 `opsResult.Failed.length`。
// 两个数**语义不同**（后端 app.go:1960 把操作前的快照与本次失败拼成并集，所以
// `GetFailedItems` 是"扫描期 ∪ 本次"，`opsResult.Failed` 只是本次）——
// 于是按钮写"失败 2（查看）"、点进去抽屉标题是"失败清单（7）"，而正文列的正是那 7 条。
//
// 修法不是把两个数拧成一个（登记的"都取 opsResult.failed"会让抽屉标题与自己列的正文
// 不同源、并把扫描期失败项从计数里抹掉，理由见设计段 §20.0-3），而是**各说各的范围**：
// 两个数不等时这条必须限定为"本次"。判据收在这里一份，组件只许引用。
export function opFailedLabel(opFailed: number, listTotal: number): string {
  if (opFailed <= 0) return '' // 本次没失败就不该有这颗按钮（全量非空是另一码事，走统计条）
  // listTotal < opFailed 在后端并集口径下不可能出现；真出现说明状态错乱，
  // 那时"限定本次"会编出一个解释不了的故事 ⇒ 按相等处理。
  const n = formatCount(opFailed)
  if (listTotal > opFailed) return `本次失败 ${n}（查看）`
  return `失败 ${n}（查看）`
}
