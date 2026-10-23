import { CircleFilled } from '@fluentui/react-icons/headless/svg/circle'
import { Switch as SwitchPrimitive } from 'radix-ui'
import type { ComponentProps, ReactNode } from 'react'
import { cn } from '@/lib/utils'

interface SwitchProps extends Omit<ComponentProps<typeof SwitchPrimitive.Root>, 'children'> {
  label: ReactNode
}

/** Fluent 2 switch (40×20 track) with its label after it; the whole row toggles. */
function Switch({ label, className, ...props }: SwitchProps) {
  return (
    <label data-slot="switch" className={cn('relative box-border inline-flex cursor-pointer items-start rounded-md has-focus-visible:outline-2 has-focus-visible:outline-(--colorStrokeFocus2)', className)}>
      <SwitchPrimitive.Root
        className={cn(
          'm-2 box-border h-5 w-10 shrink-0 cursor-pointer rounded-full border leading-0 transition-[background-color,border-color,color] duration-(--durationNormal) ease-(--curveEasyEase) outline-none motion-reduce:duration-[0.01ms]',
          'data-[state=unchecked]:border-(--colorNeutralStrokeAccessible) data-[state=unchecked]:text-(--colorNeutralStrokeAccessible) hover:data-[state=unchecked]:border-(--colorNeutralStrokeAccessibleHover) hover:data-[state=unchecked]:text-(--colorNeutralStrokeAccessibleHover) active:data-[state=unchecked]:border-(--colorNeutralStrokeAccessiblePressed) active:data-[state=unchecked]:text-(--colorNeutralStrokeAccessiblePressed)',
          'data-[state=checked]:border-transparent data-[state=checked]:bg-(--colorCompoundBrandBackground) data-[state=checked]:text-(--colorNeutralForegroundInverted) hover:data-[state=checked]:bg-(--colorCompoundBrandBackgroundHover) active:data-[state=checked]:bg-(--colorCompoundBrandBackgroundPressed)',
        )}
        {...props}
      >
        {/* Fluent draws the thumb with its 18px circle glyph. */}
        <SwitchPrimitive.Thumb className="flex size-[18px] text-[18px] leading-0 transition-transform duration-(--durationNormal) ease-(--curveEasyEase) data-[state=checked]:translate-x-5 motion-reduce:duration-[0.01ms]">
          <CircleFilled />
        </SwitchPrimitive.Thumb>
      </SwitchPrimitive.Root>
      <span className="my-0 py-2 pr-2 pl-1 text-300 leading-300 text-fg1">{label}</span>
    </label>
  )
}

export { Switch }
