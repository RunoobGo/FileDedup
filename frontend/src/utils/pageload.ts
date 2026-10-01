// M386（2026-10-01 第九轮 P1-5）：显示偏好重取时"取回已加载的那一段"的分段计划。
//
// 危害（改前形状）：refreshPendingProjection 走的是**非 append** 重取，而那条请求把
// `pageSize` 写死成 100 ⇒ 用户经 loadMore 累积到 800 组后翻一次「隐藏非拟处理项」，
// 行集塌回 100 组，`resultPage` 也跟着回到 1。第 100 组之后的勾选会被 replaySelection
// 判成"不可见"而静默丢掉 —— 那正是 M342 立"重放勾选"时要防的危害在**分页维度**上的残半
// （09 §6.1 / 10 §2.4 承诺的是"纯显示层、勾选一字不变"）。
//
// ★ 为什么不是一次大请求（设计段 §五 D1 的实施期更正）：后端 app_result.go:34-38 把
//
//	PageSize 钳到 500（M10 的溢出钉子就钉在这个值上），而放量上限初始就是 2000。
//	一次请求取不回 500 以上的行；若"按 loaded 发一次、把 resultPage 写成 ceil(loaded/100)"，
//	800 组那一档实际只回 500 行却把页号写成 8 ⇒ 下一次追加从偏移 800 起，
//	第 500~799 组**永久跳过**——修法本身会比分页塌陷更会丢数据。
//	所以这里给的是"每段不超过后端上限的若干段 + 重取后写回的页号"。
//
// ★ 判据为什么单独成文件：与 utils/selection.ts 同一手法（纯判据抽出来才能有可红的探针）。
//
//	★ selection.ts 头注释里"scan store 打不进 node --test"那句是过期的：tests/harness.mjs
//	（M16）能在 node 里实例化真 store，所以本格除纯函数判据外另有 store 级行为探针
//	（tests/pageload-projection-reload.test.ts）。
//
// ★ 页号记的是"页"不是"行"，这一点改前改后一致：追加腿每次 +100，所以 loaded 落在
//
//	非整百只可能是"结果集已到底"（最后一页回包不满），那时后面没有行可跳，向上取整无害。

/** 后端单次 pageSize 的上限（现读 app_result.go:36-38 的钳位值；两处必须一起改）。 */
export const MAX_PAGE_SIZE = 500

export interface ReloadPlan {
  /** 每段请求用的 pageSize */
  segmentSize: number
  /** 需要发几段 */
  segments: number
  /** 重取后写回 resultPage 的值（追加腿据此算下一个偏移） */
  nextPage: number
}

/**
 * 规划一次"显示偏好重取"。
 *
 * @param loaded 重取前已加载的行数（必须在发请求之前读，见调用点）
 * @param pageSize store 的常规分页步长
 * @param maxPageSize 后端单次上限，默认 MAX_PAGE_SIZE
 */
export function projectionReload(
  loaded: number,
  pageSize: number,
  maxPageSize: number = MAX_PAGE_SIZE,
): ReloadPlan {
  if (loaded <= pageSize) return { segmentSize: pageSize, segments: 1, nextPage: 1 }
  // 下限兜住"上限比步长还小"的形状：宁可回到逐页取，也不能让段大小小于 pageSize
  // 而把偏移算到已加载段中间去（后端 offset = page * pageSize）。
  const segmentSize = Math.max(pageSize, Math.min(loaded, maxPageSize))
  return {
    segmentSize,
    segments: Math.ceil(loaded / segmentSize),
    nextPage: Math.ceil(loaded / pageSize),
  }
}
