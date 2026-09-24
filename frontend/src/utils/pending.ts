// 拟处理清单的分页分界数学（C3 / R-前端-4，2026-09-24 第五轮审查）。
//
// 抽成纯函数的理由：这段"当前页里排除段从哪一行开始"的边界计算原先内联在
// PendingDrawer.vue 的 computed 里，.vue 打不进 node --test（M116/M118 同族限制），
// 边界分支（尤其"整页都被排除"）因此零覆盖。收进 utils 后既能 node 测，也让
// 组件只剩"消费一个数"。
import type { PendingPage } from '../wails'

// firstExcludedIndex 返回**当前页内**排除段（"不会被处理"）的起始下标：
//   - 整页都是将处理段（本页无排除项）        → -1（不挂分节标题）
//   - 页内某处分界（前半将处理、后半排除）     → 该下标
//   - 整页都在排除段（pending 段全在更早的页） → 0（标题置顶）
//   - 空页 / 无数据                            → -1
//
// ★ R-前端-4 修的正是第三格：pendingCount < pageStart 时 idx 为负，旧实现
// `idx >= 0 && idx < rows.length ? idx : -1` 会返回 -1 —— 于是整页灰掉的排除行
// 头上**没有**"以下均不会被处理"标题，用户看到一屏灰行却无从知道为什么。
export function firstExcludedIndex(
  d: Pick<PendingPage, 'page' | 'pageSize' | 'pendingCount' | 'rows'> | null | undefined,
): number {
  if (!d || d.rows.length === 0) return -1
  const pageStart = d.page * d.pageSize
  const idx = d.pendingCount - pageStart
  if (idx <= 0) return 0 // 整页都在排除段（含 pendingCount==0）→ 标题置顶
  if (idx < d.rows.length) return idx // 页内分界
  return -1 // 整页都是将处理段，本页无排除项
}
