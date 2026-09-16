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

export function formatMtime(ns: number): string {
  if (!ns) return '—'
  const d = new Date(ns / 1e6)
  return d.toLocaleString('zh-CN', { hour12: false })
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
