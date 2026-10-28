import { Checkmark12Filled } from '@fluentui/react-icons/headless/svg/checkmark'
import { Checkbox as CheckboxPrimitive } from 'radix-ui'
import type { ComponentProps, ReactNode } from 'react'
import { cn } from '@/lib/utils'

interface CheckboxProps extends Omit<ComponentProps<typeof CheckboxPrimitive.Root>, 'children'> {
  label: ReactNode
}

/** Fluent 2 checkbox (16px square, brand fill when checked) with its label after it; the whole row toggles. */
function Checkbox({ label, className, ...props }: CheckboxProps) {
  return (
    <label data-slot="checkbox" className={cn('flex w-fit cursor-pointer items-center gap-2 text-300 text-fg1', className)}>
      <CheckboxPrimitive.Root
        className={cn(
          'box-border inline-flex size-4 shrink-0 cursor-pointer items-center justify-center rounded-sm border text-(--colorNeutralForegroundInverted)',
          'data-[state=unchecked]:border-(--colorNeutralStrokeAccessible) hover:data-[state=unchecked]:border-(--colorNeutralStrokeAccessibleHover) active:data-[state=unchecked]:border-(--colorNeutralStrokeAccessiblePressed)',
          'data-[state=checked]:border-(--colorCompoundBrandBackground) data-[state=checked]:bg-(--colorCompoundBrandBackground) hover:data-[state=checked]:border-(--colorCompoundBrandBackgroundHover) hover:data-[state=checked]:bg-(--colorCompoundBrandBackgroundHover) active:data-[state=checked]:bg-(--colorCompoundBrandBackgroundPressed)',
          'focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-(--colorStrokeFocus2)',
        )}
        {...props}
      >
        <CheckboxPrimitive.Indicator className="inline-flex text-[12px] leading-0">
          <Checkmark12Filled />
        </CheckboxPrimitive.Indicator>
      </CheckboxPrimitive.Root>
      {label}
    </label>
  )
}

export { Checkbox }
