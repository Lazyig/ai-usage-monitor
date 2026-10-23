import type { ComponentProps } from 'react'
import { cn } from '@/lib/utils'

/** Fluent 2 large card with the Windows 11 radius and a light shadow. */
function Card({ className, ...props }: ComponentProps<'div'>) {
  return (
    <div
      data-slot="card"
      className={cn('relative box-border flex flex-col gap-4 overflow-hidden rounded-xl bg-bg1 p-4 text-fg1 shadow-2', className)}
      {...props}
    />
  )
}

export { Card }
