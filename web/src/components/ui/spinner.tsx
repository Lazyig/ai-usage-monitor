import { useId, type ReactNode } from 'react'
import { cn } from '@/lib/utils'

const SIZES = {
  'extra-tiny': 'size-4',
  tiny: 'size-5',
  small: 'size-7',
}

interface SpinnerProps {
  size?: keyof typeof SIZES
  /** Shown after the ring; also names the status for screen readers. */
  label?: ReactNode
  className?: string
}

/** Fluent 2 spinner: a brand arc chasing round a faint track (keyframes in index.css). */
function Spinner({ size = 'small', label, className }: SpinnerProps) {
  const labelId = useId()
  return (
    <span data-slot="spinner" role="progressbar" aria-labelledby={label != null ? labelId : undefined} className={cn('flex min-w-min items-center justify-center gap-2 overflow-hidden leading-0', className)}>
      <span className={cn('fluent-spinner', SIZES[size])}>
        <span className="fluent-spinner-tail" />
      </span>
      {label != null && (
        <span id={labelId} className="text-300 leading-300">
          {label}
        </span>
      )}
    </span>
  )
}

export { Spinner }
