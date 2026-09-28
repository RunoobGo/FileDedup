// M342（2026-09-28 第七轮补审 P0）：重取结果页后**回放勾选**的判据。
//
// 为什么单独成文件：scan store 依赖 pinia/vue，没法用 `frontend/tests/` 那套
// 纯 node 单测直接测；而"哪些勾选该留下来"是一个**纯判据**——抽出来才能有
// 可红的探针（与 `utils/keepsource.ts` 同一手法：把界面承诺变成可断言的函数）。
//
// 判据的两条边，缺一条都不成立：
//   ① 仍可见且可勾的行 ⇒ 勾选**一字不变**（09 §6.1 / 10 §2.4 承诺：
//      「隐藏非拟处理项」是纯显示层，翻开关不改勾选）；
//   ② 这一轮被隐藏的行 ⇒ **不再**背着勾（"已选 N"不得替看不见的东西说话）。
// 只做①会让计数替隐藏行说话；只做②就退回"全部清空"那个 P0。

/** 结果页里"可参与勾选"的最小形状（避免依赖完整 FileView 类型）。 */
export interface SelectableGroup {
  files: { id: number; isKeep?: boolean }[]
}

/**
 * 按新的可见行集回放勾选。
 *
 * @param prev 重取前的勾选快照
 * @param groups 重取后的结果页
 * @returns 回放后的勾选集合
 */
export function replaySelection(prev: Iterable<number>, groups: SelectableGroup[]): Set<number> {
  const visible = new Set<number>()
  for (const g of groups) {
    for (const f of g.files) {
      // 保留项（isKeep）始终不可勾 ⇒ 即便快照里有它也不回放
      if (!f.isKeep) visible.add(f.id)
    }
  }
  const out = new Set<number>()
  for (const id of prev) {
    if (visible.has(id)) out.add(id)
  }
  return out
}
