import type { ReactNode } from 'react'

/** A warning callout (`role="note"`), e.g. the Windows administrator
 * requirement; a `<strong>` lead-in is drawn in the warning colour.
 * `className` sets the vertical margins. */
export function Notice({ className = 'mt-1.5 mb-2', children }: { className?: string; children: ReactNode }) {
  return (
    <p
      className={`mx-0 ${className} rounded-md border border-warning-stroke bg-warning-bg px-3 py-2 text-200 leading-200 [&_strong]:text-warning-fg`}
      role="note"
    >
      {children}
    </p>
  )
}
