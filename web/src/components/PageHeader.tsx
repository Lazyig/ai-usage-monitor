import type { ReactNode } from 'react'

/** The page title row; `children` (page-level actions) sit at its far end. */
export function PageHeader({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="mt-2 mb-5 flex flex-wrap items-center justify-between gap-3 [&>h1]:m-0">
      <h1>{title}</h1>
      {children}
    </div>
  )
}
