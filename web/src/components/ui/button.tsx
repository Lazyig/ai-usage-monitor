import { cva, type VariantProps } from 'class-variance-authority'
import { Slot } from 'radix-ui'
import { forwardRef, type ComponentProps, type ReactNode } from 'react'
import { cn } from '@/lib/utils'

// Fluent 2 Button: secondary/primary/subtle appearances in small and medium,
// with the destructive fill Fluent lacks.
const buttonVariants = cva(
  'fluent-focus-inset m-0 inline-flex cursor-pointer items-center justify-center overflow-hidden rounded-md border align-middle no-underline transition-[background-color,border-color,color] duration-(--durationFaster) ease-(--curveEasyEase) hover:no-underline disabled:cursor-not-allowed motion-reduce:duration-[0.01ms]',
  {
    variants: {
      appearance: {
        secondary:
          'border-stroke1 bg-bg1 text-fg1 hover:border-(--colorNeutralStroke1Hover) hover:bg-(--colorNeutralBackground1Hover) hover:text-(--colorNeutralForeground1Hover) active:border-(--colorNeutralStroke1Pressed) active:bg-(--colorNeutralBackground1Pressed) active:text-(--colorNeutralForeground1Pressed) disabled:border-(--colorNeutralStrokeDisabled) disabled:bg-(--colorNeutralBackgroundDisabled) disabled:text-(--colorNeutralForegroundDisabled)',
        primary:
          'fluent-focus-on-brand border-transparent bg-(--colorBrandBackground) text-(--colorNeutralForegroundOnBrand) hover:bg-(--colorBrandBackgroundHover) hover:text-(--colorNeutralForegroundOnBrand) active:bg-(--colorBrandBackgroundPressed) disabled:bg-(--colorNeutralBackgroundDisabled) disabled:text-(--colorNeutralForegroundDisabled)',
        danger:
          'fluent-focus-on-brand border-transparent bg-(--colorStatusDangerBackground3) text-(--colorNeutralForegroundOnBrand) hover:bg-(--colorStatusDangerBackground3Hover) hover:text-(--colorNeutralForegroundOnBrand) active:bg-(--colorStatusDangerBackground3Pressed) disabled:bg-(--colorNeutralBackgroundDisabled) disabled:text-(--colorNeutralForegroundDisabled)',
        subtle:
          'border-transparent bg-transparent text-fg2 hover:bg-(--colorSubtleBackgroundHover) hover:text-(--colorNeutralForeground2Hover) hover:[&_[data-slot=button-icon]]:text-(--colorNeutralForeground2BrandHover) active:bg-(--colorSubtleBackgroundPressed) active:text-(--colorNeutralForeground2Pressed) disabled:bg-transparent disabled:text-(--colorNeutralForegroundDisabled) disabled:[&_[data-slot=button-icon]]:text-(--colorNeutralForegroundDisabled)',
      },
      size: {
        small: 'min-w-16 px-2 py-[3px] text-200 leading-200 font-regular',
        medium: 'min-w-24 px-3 py-[5px] text-300 leading-300 font-semibold',
      },
      iconOnly: { true: '', false: '' },
      withIcon: { true: '', false: '' },
      // Page-level actions: sized to the label, and taller on phones for touch.
      touch: { true: 'min-w-0 gap-1.5 whitespace-nowrap', false: '' },
    },
    compoundVariants: [
      { size: 'small', withIcon: true, className: 'py-px' },
      { size: 'small', iconOnly: true, className: 'max-w-6 min-w-6 p-px' },
      { size: 'medium', iconOnly: true, className: 'max-w-8 min-w-8 p-[5px]' },
      { size: 'small', touch: true, className: 'max-phone:min-h-9 max-phone:px-3 max-phone:py-1.5' },
      { size: 'medium', touch: true, className: 'max-phone:min-h-10' },
    ],
    defaultVariants: { appearance: 'secondary', size: 'medium', iconOnly: false, withIcon: false, touch: false },
  },
)

interface ButtonProps extends Omit<ComponentProps<'button'>, 'ref'>, Omit<VariantProps<typeof buttonVariants>, 'iconOnly' | 'withIcon'> {
  /** A 20px icon before the label; alone it makes a square icon button. */
  icon?: ReactNode
  /** Render the child element (a link, say) with the button's look. */
  asChild?: boolean
}

// forwardRef: Radix triggers (`asChild`) anchor popovers to the button's DOM node.
const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button({ className, appearance, size, touch, icon, asChild, children, ...props }, ref) {
  const iconOnly = icon != null && children == null
  const classes = cn(buttonVariants({ appearance, size, touch, iconOnly, withIcon: icon != null }), className)
  if (asChild) {
    return (
      <Slot.Root ref={ref} data-slot="button" className={classes} {...props}>
        {children}
      </Slot.Root>
    )
  }
  return (
    <button ref={ref} data-slot="button" type="button" className={classes} {...props}>
      {icon != null && (
        <span
          data-slot="button-icon"
          className={cn('inline-flex size-5 items-center justify-center text-[20px]', !iconOnly && (size === 'small' ? 'mr-1' : 'mr-1.5'))}
        >
          {icon}
        </span>
      )}
      {children}
    </button>
  )
})

export { Button, buttonVariants }
