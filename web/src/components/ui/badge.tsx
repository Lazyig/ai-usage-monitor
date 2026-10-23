import { cva, type VariantProps } from 'class-variance-authority'
import { forwardRef, type ComponentProps } from 'react'
import { cn } from '@/lib/utils'

// Fluent 2 rounded badge. The 1px stroke is drawn by ::after so it never adds
// to the 20px box; each colour sets the stroke through --badge-stroke.
const badgeVariants = cva(
  'relative box-border inline-flex h-5 min-w-5 items-center justify-center rounded-md px-1.5 text-200 leading-200 font-semibold after:absolute after:inset-0 after:rounded-[inherit] after:border after:border-(--badge-stroke) after:content-[""]',
  {
    variants: {
      color: {
        brand: '[--badge-stroke:var(--colorBrandStroke2)] bg-(--colorBrandBackground2) text-(--colorBrandForeground2)',
        subtle: '[--badge-stroke:var(--colorNeutralStroke2)] bg-bg1 text-fg3',
        informative: '[--badge-stroke:var(--colorNeutralStroke2)] bg-(--colorNeutralBackground4) text-fg3',
        warning: '[--badge-stroke:var(--colorPaletteYellowBorder1)] bg-(--colorPaletteYellowBackground1) text-(--colorPaletteYellowForeground1)',
        severe: '[--badge-stroke:var(--colorPaletteDarkOrangeBorder1)] bg-(--colorPaletteDarkOrangeBackground1) text-(--colorPaletteDarkOrangeForeground1)',
        danger: '[--badge-stroke:var(--colorPaletteRedBorder1)] bg-(--colorPaletteRedBackground1) text-(--colorPaletteRedForeground1)',
      },
      appearance: {
        tint: '',
        // Outline badges are transparent; only the informative outline is used.
        outline: '[--badge-stroke:var(--colorNeutralStroke2)] bg-transparent text-fg3',
      },
    },
    defaultVariants: { color: 'informative', appearance: 'tint' },
  },
)

type BadgeProps = Omit<ComponentProps<'span'>, 'color' | 'ref'> & VariantProps<typeof badgeVariants>

// forwardRef so a badge can be a tooltip trigger.
const Badge = forwardRef<HTMLSpanElement, BadgeProps>(function Badge({ className, color, appearance, ...props }, ref) {
  return <span ref={ref} data-slot="badge" className={cn(badgeVariants({ color, appearance }), className)} {...props} />
})

export { Badge, badgeVariants }
