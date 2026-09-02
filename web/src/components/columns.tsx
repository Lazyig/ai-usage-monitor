// Column factories for `DataTable`. Every usage table shows the same handful
// of cell shapes — a compact token count with the exact value on hover, a USD
// amount that may be unknown, an integer count, a timestamp — so they are
// defined once here instead of being retyped per page.
import { Badge } from './Badge'
import type { Column } from './DataTable'
import { formatCompact, formatDateTime, formatFull, formatUsd } from '../lib/format'

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
 * it as 단가 미상. Unknown sorts below zero so unpriced rows sink to the
 * bottom of a descending cost sort rather than mixing in with free ones.
 */
export function costColumn<T>(
  key: string,
  header: string,
  value: (row: T) => number | null,
  opts: { badge?: boolean; sortValue?: (row: T) => number } = {},
): Column<T> {
  return {
    key,
    header,
    align: 'right',
    render: opts.badge
      ? (r) => (
          <span>
            {formatUsd(value(r))} {value(r) === null && <Badge kind="unknown" />}
          </span>
        )
      : (r) => formatUsd(value(r)),
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

/** Strips a factory column's sorter. For tables that are ordered by the server
 * and paginated by cursor, where re-sorting the one loaded page would
 * misrepresent the whole result set. */
export function unsorted<T>(col: Column<T>): Column<T> {
  return { ...col, sortValue: undefined }
}
