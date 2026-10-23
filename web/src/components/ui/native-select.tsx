import type { ComponentProps, CSSProperties } from 'react'
import { cn } from '@/lib/utils'

const CHEVRON = (fill: string) =>
  `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 12 12'%3E%3Cpath d='M2.2 4.2a.75.75 0 0 1 1.06 0L6 6.94l2.74-2.74a.75.75 0 1 1 1.06 1.06L6.53 8.53a.75.75 0 0 1-1.06 0L2.2 5.26a.75.75 0 0 1 0-1.06Z' fill='%23${fill}'/%3E%3C/svg%3E")`

/**
 * Native <select> drawn as a Fluent 2 dropdown: the input frame with a
 * chevron, and the brand underline when focused. Native so phones get their
 * own picker.
 */
function NativeSelect({ className, style, ...props }: ComponentProps<'select'>) {
  return (
    <select
      data-slot="native-select"
      style={{ '--chevron-light': CHEVRON('616161'), '--chevron-dark': CHEVRON('adadad'), ...style } as CSSProperties}
      className={cn(
        'h-8 cursor-pointer appearance-none rounded-md border border-stroke1 border-b-(--colorNeutralStrokeAccessible) bg-bg1 bg-(image:--chevron-light) bg-size-[12px] bg-position-[right_10px_center] bg-no-repeat py-0 pr-[30px] pl-2.5 text-300 text-fg1 dark:bg-(image:--chevron-dark)',
        'hover:border-(--colorNeutralStroke1Hover) hover:border-b-(--colorNeutralStrokeAccessibleHover)',
        'focus:border-b-(--colorCompoundBrandStroke) focus:shadow-[inset_0_-1px_0_var(--colorCompoundBrandStroke)] focus:outline-none',
        className,
      )}
      {...props}
    />
  )
}

export { NativeSelect }
