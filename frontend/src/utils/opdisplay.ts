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

import { humanBytes } from './format'
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
    default:
      return { lead: '共 ', bytes: n, tail: ' 空间可释放' }
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
