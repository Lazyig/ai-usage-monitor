import type { ComponentProps, ReactNode } from 'react'
import { cn } from '@/lib/utils'

interface CheckboxProps extends Omit<ComponentProps<'input'>, 'type'> {
  label: ReactNode
}

/** Native checkbox in the brand colour, with its label after it; the whole row toggles. */
function Checkbox({ label, className, ...props }: CheckboxProps) {
  return (
    <label data-slot="checkbox" className={cn('flex cursor-pointer items-center gap-2 text-300 text-fg1', className)}>
      <input
        type="checkbox"
        className="m-0 size-4 accent-(--colorCompoundBrandBackground) focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-(--colorStrokeFocus2)"
        {...props}
      />
      {label}
    </label>
  )
}

export { Checkbox }
