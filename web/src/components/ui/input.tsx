import type { ComponentProps, ReactNode } from 'react'
import { cn } from '@/lib/utils'

interface InputProps extends Omit<ComponentProps<'input'>, 'size'> {
  size?: 'small' | 'medium'
  /** Inside the frame after the text, like Fluent's contentAfter slot (a button, say). */
  contentAfter?: ReactNode
}

/**
 * Fluent 2 outline input: a framed box whose bottom edge is the accessible
 * stroke, with the brand underline sweeping in on focus. `className` styles
 * the frame; everything else goes to the <input>.
 */
function Input({ className, size = 'medium', contentAfter, ...props }: InputProps) {
  return (
    <span
      data-slot="input"
      className={cn(
        'relative box-border inline-flex flex-nowrap items-center gap-0.5 rounded-md border border-stroke1 border-b-(--colorNeutralStrokeAccessible) bg-bg1 align-middle',
        'hover:border-(--colorNeutralStroke1Hover) hover:border-b-(--colorNeutralStrokeAccessibleHover)',
        'focus-within:border-(--colorNeutralStroke1Pressed) focus-within:border-b-(--colorNeutralStrokeAccessiblePressed) focus-within:outline-2 focus-within:outline-transparent active:border-(--colorNeutralStroke1Pressed) active:border-b-(--colorNeutralStrokeAccessiblePressed)',
        'aria-invalid:not-focus-within:border-(--colorPaletteRedBorder2)',
        // The underline: clipped to its bottom 2px and scaled in from the centre.
        'after:absolute after:-right-px after:-bottom-px after:-left-px after:box-border after:h-[max(2px,var(--borderRadiusMedium))] after:scale-x-0 after:rounded-b-md after:border-b-2 after:border-(--colorCompoundBrandStroke) after:transition-transform after:duration-(--durationUltraFast) after:ease-(--curveAccelerateMid) after:content-[""] after:[clip-path:inset(calc(100%-2px)_0_0_0)]',
        'focus-within:after:scale-x-100 focus-within:after:duration-(--durationNormal) focus-within:after:ease-(--curveDecelerateMid) focus-within:active:after:border-(--colorCompoundBrandStrokePressed) motion-reduce:after:duration-[0.01ms]',
        size === 'small' ? 'min-h-6 text-200 leading-200' : 'min-h-8 text-300 leading-300',
        className,
      )}
      aria-invalid={props['aria-invalid']}
    >
      <input
        className={cn(
          'box-border min-w-0 grow self-stretch border-none bg-transparent text-fg1 outline-none [font:inherit] placeholder:text-fg4 placeholder:opacity-100 disabled:cursor-not-allowed disabled:text-(--colorNeutralForegroundDisabled)',
          size === 'small' ? 'px-2' : 'px-3',
        )}
        {...props}
      />
      {contentAfter}
    </span>
  )
}

export { Input }
