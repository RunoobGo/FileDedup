// 展示格式化工具。

export function humanBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return '0 B'
  const u = 1024
  if (n < u) return `${n} B`
  const exp = Math.floor(Math.log(n) / Math.log(u))
  const div = Math.pow(u, exp)
  const unit = 'KMGTPE'[exp - 1] ?? 'K'
  return `${(n / div).toFixed(1)} ${unit}B`
}

export function humanSpeed(bps: number): string {
  if (bps <= 0) return '—'
  return `${humanBytes(bps)}/s`
}

export function humanTime(sec: number): string {
  if (sec < 0) return '—'
  if (sec < 60) return `${Math.ceil(sec)} 秒`
  if (sec < 3600) return `${Math.floor(sec / 60)} 分 ${Math.floor(sec % 60)} 秒`
  return `${Math.floor(sec / 3600)} 时 ${Math.floor((sec % 3600) / 60)} 分`
}

export function humanTimeShort(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`
  return `${Math.floor(ms / 60000)}m${Math.floor((ms % 60000) / 1000)}s`
}

/** P2-6：计数类整数统一加千分位。
 *  此前 12 万条缓存显示为「128394 条目」，位数一多就难以一眼读出量级。 */
export function formatCount(n: number | undefined | null): string {
  if (n === undefined || n === null || !Number.isFinite(n)) return '—'
  return n.toLocaleString('zh-CN')
}

/** 三个时间格式化函数的唯一实现：毫秒 → 本地时间串（24 小时制）。 */
function localTimeFromMs(ms: number): string {
  return new Date(ms).toLocaleString('zh-CN', { hour12: false })
}

export function formatMtime(ns: number): string {
  if (!ns) return '—'
  return localTimeFromMs(ns / 1e6)
}

/** 秒级 Unix 时间戳（历史记录的 savedAt / 操作记录的时间字段）→ 本地时间串。
 *  M116：此前 ScanView / RecordsView / ResultView 各写一遍同一句内联串，与这里
 *  的 `formatMtime` 是同一件事的第二、三、四份实现。收归后两处措辞只能一起改。
 *  ★ 与 `formatMtime` 的差别是**故意的**：那条把 0 当作「mtime 未知」显示 `—`，
 *  这里不夹 0，与三条被替换的内联串严格等价（本批只挪位置，不改行为）。 */
export function formatUnixSec(sec: number): string {
  return localTimeFromMs(sec * 1000)
}

export const stageLabel: Record<string, string> = {
  scan: '扫描目录',
  prefilter: '预筛哈希',
  hash: '全量哈希',
  verify: '逐字节确认',
}

export const statusLabel: Record<string, string> = {
  Idle: '空闲',
  Scanning: '扫描中',
  Prefiltering: '预筛中',
  Hashing: '哈希中',
  Paused: '已暂停',
  Done: '已完成',
  Cancelled: '已取消',
  Failed: '失败',
}
