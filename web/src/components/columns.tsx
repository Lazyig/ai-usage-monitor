// Column factories for `DataTable`. Every usage table shows the same handful
// of cell shapes — a compact token count with the exact value on hover, a USD
// amount that may be unknown, an integer count, a timestamp — so they are
// defined once here instead of being retyped per page.
import { Badge } from './Badge'
import type { Column } from './DataTable'
import { formatCompact, formatDateTime, formatFull, formatUsd } from '../lib/format'
import { pricedShareLabel } from '../lib/usage'

/** Right-aligned compact token count ("1.2M"), exact value on hover. */
export function tokenColumn<T>(key: string, header: string, value: (row: T) => number): Column<T> {
  return {
    key,
    header,
    align: 'right',
    render: (r) => <span title={formatFull(value(r))}>{formatCompact(value(r))}</span>,
    sortValue: value,
  }
}

/** Right-aligned integer with thousands separators (event counts). */
export function countColumn<T>(key: string, header: string, value: (row: T) => number): Column<T> {
  return { key, header, align: 'right', render: (r) => formatFull(value(r)), sortValue: value }
}

/**
 * Right-aligned USD amount. `null` renders as "—"; pass `badge` to also mark
 * it as 단가 미상. Pass `ratio` (the row's cost_known_ratio) to flag a row only
 * part of which had a price, so its amount is not read as the full cost.
 * Unknown sorts below zero so unpriced rows sink to the bottom of a
 * descending cost sort rather than mixing in with free ones.
 */
export function costColumn<T>(
  key: string,
  header: string,
  value: (row: T) => number | null,
  opts: { badge?: boolean; ratio?: (row: T) => number | undefined; sortValue?: (row: T) => number } = {},
): Column<T> {
  return {
    key,
    header,
    align: 'right',
    render: (r) => {
      const v = value(r)
      const ratio = opts.ratio?.(r)
      const partial = v !== null && ratio !== undefined && ratio > 0 && ratio < 1
      if (!(opts.badge && v === null) && !partial) return formatUsd(v)
      return (
        <span>
          {formatUsd(v)}{' '}
          {v === null ? <Badge kind="unknown" /> : <Badge kind="warning">{pricedShareLabel(ratio as number)}</Badge>}
        </span>
      )
    },
    sortValue: opts.sortValue ?? ((r) => value(r) ?? -1),
  }
}

/** Absolute timestamp in KST. `fallback` replaces the default "-" dash. */
export function dateTimeColumn<T>(
  key: string,
  header: string,
  value: (row: T) => string | undefined,
  opts: { fallback?: string } = {},
): Column<T> {
  return {
    key,
    header,
    render: (r) => (opts.fallback !== undefined && !value(r) ? opts.fallback : formatDateTime(value(r))),
    sortValue: (r) => value(r) ?? '',
  }
}

/** Leading color chip for a cell layout, matching the series color in the charts. */
export function Swatch({ color }: { color: string }) {
  return <span className="inline-block size-3 rounded-sm" style={{ background: color }} aria-hidden="true" />
}

/** Strips a factory column's sorter. For tables that are ordered by the server
 * and paginated by cursor, where re-sorting the one loaded page would
 * misrepresent the whole result set. */
export function unsorted<T>(col: Column<T>): Column<T> {
  return { ...col, sortValue: undefined }
}
