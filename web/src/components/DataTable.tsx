import {
  Table,
  TableBody,
  TableCell,
  TableCellActions,
  TableCellLayout,
  TableHeader,
  TableHeaderCell,
  TableRow,
} from '@fluentui/react-components'
import { useMemo, useState, type ReactNode } from 'react'
import { useMediaQuery } from '../hooks/useMediaQuery'

export interface Column<T> {
  key: string
  header: string
  render: (row: T) => ReactNode
  sortValue?: (row: T) => number | string
  align?: 'left' | 'right'
  /** Fluent cell layout: a leading icon or swatch before the content. */
  media?: (row: T) => ReactNode
  /** Fluent cell layout: a secondary line under the content. */
  description?: (row: T) => ReactNode
  /** Row actions shown inside this cell when the row is hovered or focused. */
  actions?: (row: T) => ReactNode
}

interface DataTableProps<T> {
  columns: Column<T>[]
  rows: T[]
  rowKey: (row: T) => string
  initialSortKey?: string
  initialSortDir?: 'asc' | 'desc'
  /** Fluent row density: medium (44px) for summary tables, small (34px) for long logs. */
  size?: 'small' | 'medium'
}

/** Fluent slots take an element or nothing, not React's booleans: empty means no slot. */
function slot(node: ReactNode) {
  return node == null || typeof node === 'boolean' ? undefined : <>{node}</>
}

export function DataTable<T>({ columns, rows, rowKey, initialSortKey, initialSortDir = 'desc', size = 'medium' }: DataTableProps<T>) {
  const [sortKey, setSortKey] = useState<string | undefined>(initialSortKey)
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>(initialSortDir)
  // Hover-revealed actions would be unreachable on touch screens, so show them there.
  const noHover = useMediaQuery('(hover: none)')

  const sorted = useMemo(() => {
    const col = columns.find((c) => c.key === sortKey)
    if (!col?.sortValue) return rows
    const sv = col.sortValue
    const copy = [...rows]
    copy.sort((a, b) => {
      const av = sv(a)
      const bv = sv(b)
      if (av < bv) return sortDir === 'asc' ? -1 : 1
      if (av > bv) return sortDir === 'asc' ? 1 : -1
      return 0
    })
    return copy
  }, [rows, sortKey, sortDir, columns])

  function onHeaderClick(col: Column<T>) {
    if (!col.sortValue) return
    if (sortKey === col.key) {
      setSortDir((d) => (d === 'asc' ? 'desc' : 'asc'))
    } else {
      setSortKey(col.key)
      setSortDir('desc')
    }
  }

  function renderCell(col: Column<T>, row: T) {
    const content =
      col.media || col.description ? (
        <TableCellLayout media={slot(col.media?.(row))} description={slot(col.description?.(row))}>
          {col.render(row)}
        </TableCellLayout>
      ) : (
        col.render(row)
      )
    return (
      <>
        {content}
        {col.actions && <TableCellActions visible={noHover || undefined}>{col.actions(row)}</TableCellActions>}
      </>
    )
  }

  return (
    <div className="table-scroll">
      <Table size={size} sortable={columns.some((c) => c.sortValue)} className="data-table">
        <TableHeader>
          <TableRow>
            {columns.map((col) => (
              <TableHeaderCell
                key={col.key}
                className={col.align === 'right' ? 'ta-right' : undefined}
                onClick={col.sortValue ? () => onHeaderClick(col) : undefined}
                sortDirection={sortKey === col.key ? (sortDir === 'asc' ? 'ascending' : 'descending') : undefined}
                data-sortable={col.sortValue ? 'true' : undefined}
              >
                {col.header}
              </TableHeaderCell>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {sorted.map((row) => (
            <TableRow key={rowKey(row)}>
              {columns.map((col) => (
                <TableCell key={col.key} className={col.align === 'right' ? 'ta-right' : col.actions ? 'has-actions' : undefined}>
                  {renderCell(col, row)}
                </TableCell>
              ))}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
