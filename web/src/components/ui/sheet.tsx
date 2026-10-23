import { Dialog as SheetPrimitive } from 'radix-ui'
import type { ComponentProps } from 'react'
import { cn } from '@/lib/utils'
import { DialogOverlay } from './dialog'

const Sheet = SheetPrimitive.Root

interface SheetContentProps extends ComponentProps<typeof SheetPrimitive.Content> {
  /** Screen-reader name for the panel, since it has no visible heading. */
  title: string
}

/** Fluent 2 overlay drawer: a panel sliding in from the start edge over a dimmed page. */
function SheetContent({ title, className, children, ...props }: SheetContentProps) {
  return (
    <SheetPrimitive.Portal>
      <DialogOverlay />
      <SheetPrimitive.Content
        data-slot="sheet-content"
        aria-describedby={undefined}
        className={cn(
          'fixed inset-y-0 left-0 z-50 flex max-w-[100vw] flex-col overflow-hidden shadow-64 outline-none',
          'duration-(--durationGentle) ease-(--curveDecelerateMid) data-[state=closed]:animate-out data-[state=closed]:slide-out-to-left data-[state=closed]:ease-(--curveAccelerateMid) data-[state=open]:animate-in data-[state=open]:slide-in-from-left',
          className,
        )}
        {...props}
      >
        <SheetPrimitive.Title className="sr-only">{title}</SheetPrimitive.Title>
        {children}
      </SheetPrimitive.Content>
    </SheetPrimitive.Portal>
  )
}

export { Sheet, SheetContent }
