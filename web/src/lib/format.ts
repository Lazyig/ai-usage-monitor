// Number, currency, and relative-time formatting helpers shared by every page.

const compactFormatter = new Intl.NumberFormat('en-US', { notation: 'compact', maximumFractionDigits: 1 })
const fullFormatter = new Intl.NumberFormat('en-US')

/** 1.2M / 340K style compact token count. Pass the full value as a title attr separately. */
export function formatCompact(n: number): string {
  return compactFormatter.format(n)
}

/** Full value with thousands separators, for title/hover attributes. */
export function formatFull(n: number): string {
  return fullFormatter.format(n)
}

/** USD with 2 decimals, 4 decimals when the magnitude is below 0.01. null means unknown price. */
export function formatUsd(n: number | null | undefined): string {
  if (n === null || n === undefined) return '—'
  const abs = Math.abs(n)
  const decimals = abs > 0 && abs < 0.01 ? 4 : 2
  return `$${n.toFixed(decimals)}`
}

export function formatPercent(ratio: number, decimals = 0): string {
  return `${(ratio * 100).toFixed(decimals)}%`
}

const RTF = new Intl.RelativeTimeFormat('ko', { numeric: 'auto' })

/** Korean relative time ("3분 전", "방금 전") for a timestamp. */
export function formatRelative(iso: string | undefined | null): string {
  if (!iso) return '알 수 없음'
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return '알 수 없음'
  const diffMs = then - Date.now()
  const diffSec = Math.round(diffMs / 1000)
  const abs = Math.abs(diffSec)

  if (abs < 45) return '방금 전'
  const units: [Intl.RelativeTimeFormatUnit, number][] = [
    ['year', 31536000],
    ['month', 2592000],
    ['week', 604800],
    ['day', 86400],
    ['hour', 3600],
    ['minute', 60],
  ]
  for (const [unit, secs] of units) {
    if (abs >= secs || unit === 'minute') {
      return RTF.format(Math.round(diffSec / secs), unit)
    }
  }
  return '방금 전'
}

/** True when the timestamp is more than `thresholdMinutes` in the past (device heartbeat staleness). */
export function isStale(iso: string | undefined | null, thresholdMinutes = 10): boolean {
  if (!iso) return true
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return true
  return Date.now() - then > thresholdMinutes * 60_000
}

export function formatDateTime(iso: string | undefined | null): string {
  if (!iso) return '-'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '-'
  return new Intl.DateTimeFormat('ko-KR', {
    timeZone: 'Asia/Seoul',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(d)
}

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let v = bytes
  let i = -1
  do {
    v /= 1024
    i++
  } while (v >= 1024 && i < units.length - 1)
  return `${v.toFixed(1)} ${units[i]}`
}

/** Shortens project keys (paths/URIs) to their last two path segments; caller shows the full value via title=. */
export function shortenProjectKey(key: string): string {
  const withoutScheme = key.replace(/^[a-zA-Z][a-zA-Z0-9+.-]*:\/\//, '')
  const segments = withoutScheme.split('/').filter(Boolean)
  if (segments.length <= 2) return withoutScheme || key
  return `.../${segments.slice(-2).join('/')}`
}
