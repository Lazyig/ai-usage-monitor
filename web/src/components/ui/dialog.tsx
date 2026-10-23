import { Dialog as DialogPrimitive } from 'radix-ui'
import type { ComponentProps } from 'react'
import { cn } from '@/lib/utils'

const Dialog = DialogPrimitive.Root

function DialogOverlay({ className, ...props }: ComponentProps<typeof DialogPrimitive.Overlay>) {
  return (
    <DialogPrimitive.Overlay
      data-slot="dialog-overlay"
      className={cn(
        'fixed inset-0 z-50 bg-(--colorBackgroundOverlay) duration-(--durationGentle) data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:animate-in data-[state=open]:fade-in-0',
        className,
      )}
      {...props}
    />
  )
}

/**
 * Fluent 2 dialog surface: a 600px card centred on a dimmed page that scales
 * in, with Fluent's title / content / actions grid inside. Below 480px it
 * spans the width and stacks the actions.
 */
function DialogContent({ className, children, ...props }: ComponentProps<typeof DialogPrimitive.Content>) {
  return (
    <DialogPrimitive.Portal>
      <DialogOverlay />
      <DialogPrimitive.Content
        data-slot="dialog-content"
        aria-describedby={undefined}
        className={cn(
          'fixed inset-0 z-50 m-auto box-border block h-fit max-h-dvh max-w-[600px] rounded-xl border border-transparent bg-bg1 p-6 text-fg1 shadow-64 outline-none max-[480px]:max-w-[100vw]',
          'duration-(--durationGentle) ease-(--curveDecelerateMid) data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-[0.85] data-[state=closed]:ease-(--curveAccelerateMin) data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-[0.85]',
          className,
        )}
        {...props}
      >
        <div className="box-border grid max-h-[calc(100dvh-48px)] grid-cols-[1fr_1fr_auto] grid-rows-[auto_1fr] gap-2 max-[480px]:max-w-[100vw] max-[480px]:grid-rows-[auto_1fr_auto]">
          {children}
        </div>
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  )
}

function DialogTitle({ className, ...props }: ComponentProps<typeof DialogPrimitive.Title>) {
  return (
    <DialogPrimitive.Title
      data-slot="dialog-title"
      className={cn('col-span-3 row-start-1 m-0 text-500 leading-500 font-semibold text-fg1', className)}
      {...props}
    />
  )
}

/** The scrollable body; the 2px padding keeps focus outlines inside the scroll box. */
function DialogBody({ className, ...props }: ComponentProps<'div'>) {
  return <div data-slot="dialog-body" className={cn('col-span-3 row-start-2 -m-0.5 box-border min-h-8 overflow-y-auto p-0.5 text-300 leading-300', className)} {...props} />
}

function DialogActions({ className, ...props }: ComponentProps<'div'>) {
  return (
    <div
      data-slot="dialog-actions"
      className={cn(
        'col-start-2 col-end-4 row-start-3 box-border flex h-fit gap-2 justify-self-end max-[480px]:col-start-1 max-[480px]:row-start-4 max-[480px]:flex-col max-[480px]:justify-self-stretch',
        className,
      )}
      {...props}
    />
  )
}

export { Dialog, DialogActions, DialogBody, DialogContent, DialogOverlay, DialogTitle }
