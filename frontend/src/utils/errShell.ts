// 错误事件载荷 → 用户可读文本（M202，登记 §6.29，设计稿 `2026-09-24-error-shell-m202`）。
//
// 后端 `errorEvent` 产出的载荷是 `{error, detail?}`：`error` 是中文外壳，`detail` 是
// 系统原文（含绝对路径）。这里负责把它拼成一行提示：**中文在前、原文在后且标明是原文**。
//
// 为什么还要显示原文：外壳是为了读得懂，原文是为了排查与"复制这条错误"，两者不可互替；
// 降级（而不是删掉）才是本次裁定「统一中文外壳 + 原文降级为 detail」的字面含义。
//
// 兼容四种输入（后端可能仍是裸字符串的第三方路径，前端本地抛的 Error 也走这里）：
//   {error, detail} / {error} / 裸字符串 / Error 实例。

export function errWithDetail(e: unknown): string {
  if (e === null || e === undefined) return ''
  if (typeof e === 'string') return e
  if (e instanceof Error) return e.message
  if (typeof e === 'object') {
    const rec = e as Record<string, unknown>
    const shell = 'error' in rec ? String(rec.error) : ''
    const detail = 'detail' in rec && rec.detail !== undefined && rec.detail !== null ? String(rec.detail) : ''
    if (!shell) return detail
    // detail 与外壳同值（后端 B 类：应用自撰中文原样透出）时不重复一遍。
    if (!detail || detail === shell) return shell
    return `${shell}（系统原文：${detail}）`
  }
  return String(e)
}
