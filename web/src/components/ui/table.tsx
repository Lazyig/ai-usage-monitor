import type { ComponentProps } from 'react'
import { cn } from '@/lib/utils'

/** Fluent 2 table: transparent, collapsed borders, 1px dividers between rows. */
function Table({ className, ...props }: ComponentProps<'table'>) {
  return <table data-slot="table" className={cn('w-full border-collapse align-middle', className)} {...props} />
}

function TableHeader(props: ComponentProps<'thead'>) {
  return <thead data-slot="table-header" {...props} />
}

function TableBody(props: ComponentProps<'tbody'>) {
  return <tbody data-slot="table-body" {...props} />
}

/** Body rows highlight on hover and reveal their cell actions. */
function TableRow({ className, ...props }: ComponentProps<'tr'>) {
  return (
    <tr
      data-slot="table-row"
      className={cn(
        'group/row box-border border-b border-stroke2 text-fg1 in-data-[slot=table-body]:hover:bg-(--colorSubtleBackgroundHover) in-data-[slot=table-body]:hover:text-(--colorNeutralForeground1Hover) in-data-[slot=table-body]:active:bg-(--colorSubtleBackgroundPressed) in-data-[slot=table-body]:active:text-(--colorNeutralForeground1Pressed)',
        className,
      )}
      {...props}
    />
  )
}

function TableHead({ className, ...props }: ComponentProps<'th'>) {
  return (
    <th
      data-slot="table-head"
      className={cn('relative px-2 text-left align-middle font-semibold text-fg2 has-focus-visible:outline-2 has-focus-visible:-outline-offset-2 has-focus-visible:outline-(--colorStrokeFocus2)', className)}
      {...props}
    />
  )
}

function TableCell({ className, ...props }: ComponentProps<'td'>) {
  return <td data-slot="table-cell" className={cn('relative px-2 align-middle', className)} {...props} />
}

export { Table, TableBody, TableCell, TableHead, TableHeader, TableRow }
