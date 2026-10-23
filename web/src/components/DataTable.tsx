import { ArrowDownRegular } from '@fluentui/react-icons/headless/svg/arrow-down'
import { ArrowUpRegular } from '@fluentui/react-icons/headless/svg/arrow-up'
import { useMemo, useState, type ReactNode } from 'react'
import { useMediaQuery } from '../hooks/useMediaQuery'
import { cn } from '../lib/utils'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from './ui/table'

export interface Column<T> {
  key: string
  header: string
  render: (row: T) => ReactNode
  sortValue?: (row: T) => number | string
  align?: 'left' | 'right'
  /** Cell layout: a leading icon or swatch before the content. */
  media?: (row: T) => ReactNode
  /** Cell layout: a secondary line under the content. */
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
  /** Row density: medium (44px) for summary tables, small (34px) for long logs. */
  size?: 'small' | 'medium'
}

/** React's booleans render nothing, so they count as an empty slot. */
function isEmpty(node: ReactNode) {
  return node == null || typeof node === 'boolean'
}

// The identifying first column stays pinned while a narrow table scrolls sideways.
const PINNED_FIRST = 'max-narrow:first:sticky max-narrow:first:left-0 max-narrow:first:z-1 max-narrow:first:bg-bg1 max-narrow:first:shadow-[1px_0_0_var(--colorNeutralStroke2)]'

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
    const media = col.media?.(row)
    const description = col.description?.(row)
    const content =
      col.media || col.description ? (
        <div className="flex flex-1 items-center gap-2">
          {!isEmpty(media) && <div className="flex items-center text-[20px] leading-[20px] text-fg2">{media}</div>}
          <div className="flex flex-col">
            <div>{col.render(row)}</div>
            {!isEmpty(description) && <div className="text-200 leading-200 text-fg3">{description}</div>}
          </div>
        </div>
      ) : (
        col.render(row)
      )
    return (
      <>
        {content}
        {col.actions && (
          // Revealed while the row is hovered, holds keyboard focus or has one of its menus open.
          <div
            className={cn(
              'absolute top-1/2 right-0 ml-auto -translate-y-1/2 bg-inherit opacity-0 group-hover/row:opacity-100 group-has-focus-visible/row:opacity-100 group-has-data-[state=open]/row:opacity-100',
              noHover && 'opacity-100',
            )}
          >
            {col.actions(row)}
          </div>
        )}
      </>
    )
  }

  return (
    // A table standing on the page gets a card surface; inside a card it is flat.
    <div className="max-w-full overflow-x-auto rounded-xl bg-bg1 px-2 py-1 shadow-2 in-data-[slot=card]:rounded-none in-data-[slot=card]:bg-transparent in-data-[slot=card]:p-0 in-data-[slot=card]:shadow-none">
      <Table className="table-auto">
        <TableHeader>
          <TableRow>
            {columns.map((col) => {
              const label = (
                <>
                  {col.header}
                  {sortKey === col.key && (
                    <span className="flex items-center pt-0.5 text-[12px]">{sortDir === 'asc' ? <ArrowUpRegular /> : <ArrowDownRegular />}</span>
                  )}
                </>
              )
              const layout = cn('relative flex h-full min-h-8 w-full flex-1 items-center gap-1 outline-none', col.align === 'right' && 'justify-end')
              return (
                <TableHead
                  key={col.key}
                  className={cn(
                    'whitespace-nowrap tabular-nums',
                    col.align === 'right' && 'text-right',
                    col.sortValue &&
                      'hover:bg-(--colorSubtleBackgroundHover) hover:text-(--colorNeutralForeground1Hover) active:bg-(--colorSubtleBackgroundPressed) active:text-(--colorNeutralForeground1Pressed)',
                    PINNED_FIRST,
                  )}
                  aria-sort={sortKey === col.key ? (sortDir === 'asc' ? 'ascending' : 'descending') : undefined}
                >
                  {col.sortValue ? (
                    <button type="button" className={cn(layout, 'cursor-pointer text-left')} onClick={() => onHeaderClick(col)}>
                      {label}
                    </button>
                  ) : (
                    <div className={layout}>{label}</div>
                  )}
                </TableHead>
              )
            })}
          </TableRow>
        </TableHeader>
        <TableBody>
          {sorted.map((row) => (
            <TableRow key={rowKey(row)}>
              {columns.map((col) => (
                <TableCell
                  key={col.key}
                  className={cn(
                    'whitespace-nowrap tabular-nums',
                    size === 'small' ? 'h-[34px]' : 'h-11',
                    col.align === 'right' ? 'text-right' : col.actions && 'min-w-[200px] pr-20',
                    PINNED_FIRST,
                  )}
                >
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
