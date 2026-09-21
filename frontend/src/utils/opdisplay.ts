// 展示层措辞（M15 起）：凡"某个字节数对用户意味着什么"的句子，集中在这里一份。
//
// 为什么集中：这类句子以前散在确认框、结果条、组卡片里各自拼字符串，
// 于是出现过"确认框说可释放 X、结果条说占用不变"的自相矛盾（M15）。
// 判据只有一份原则同样适用于文案。
//
// 口径来源是后端 `internal/ops/executor.go` 的 aggregate()：
//   hardlink → LinkedBytes（数据块变共享，用户看到的文件夹占用不变）
//   symlink  → SymlinkedBytes（磁盘上确实少一整份，但保留项仍可访问）
//   其余     → Reclaimed（数据真的从磁盘消失）
// 这里的措辞必须与那三个栏位一一对应，改任何一处都要看另一处。

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
    default:
      return { lead: '共 ', bytes: n, tail: ' 空间可释放' }
  }
}
