import { CheckmarkFilled } from '@fluentui/react-icons/headless/svg/checkmark'
import { ChevronDown12Regular } from '@fluentui/react-icons/headless/svg/chevron-down'
import { Select as SelectPrimitive } from 'radix-ui'
import { cn } from '@/lib/utils'

// Radix reserves '' for "no value", but the filters use it for "all".
const EMPTY = '\u0000all'

interface SelectOption {
  value: string
  label: string
}

interface SelectProps {
  id?: string
  value: string
  onValueChange: (value: string) => void
  options: SelectOption[]
  className?: string
  'aria-invalid'?: boolean
  'aria-describedby'?: string
}

/**
 * Fluent 2 outline dropdown: the input frame with a chevron and the brand
 * underline when focused, opening a Fluent listbox (checkmark on the selected
 * option) instead of the browser's own popup, which ignores the theme.
 */
function Select({ id, value, onValueChange, options, className, ...aria }: SelectProps) {
  return (
    <SelectPrimitive.Root value={value === '' ? EMPTY : value} onValueChange={(v) => onValueChange(v === EMPTY ? '' : v)}>
      <SelectPrimitive.Trigger
        id={id}
        data-slot="select"
        className={cn(
          'relative box-border inline-flex h-8 cursor-pointer items-center justify-between gap-2 rounded-md border border-stroke1 border-b-(--colorNeutralStrokeAccessible) bg-bg1 px-2.5 py-0 text-left text-300 text-fg1 outline-none',
          'hover:border-(--colorNeutralStroke1Hover) hover:border-b-(--colorNeutralStrokeAccessibleHover)',
          'focus-visible:border-b-(--colorCompoundBrandStroke) focus-visible:shadow-[inset_0_-1px_0_var(--colorCompoundBrandStroke)] data-[state=open]:border-b-(--colorCompoundBrandStroke) data-[state=open]:shadow-[inset_0_-1px_0_var(--colorCompoundBrandStroke)]',
          className,
        )}
        {...aria}
      >
        <span className="min-w-0 truncate">
          <SelectPrimitive.Value />
        </span>
        <SelectPrimitive.Icon className="inline-flex size-3 shrink-0 items-center justify-center text-[12px] leading-0 text-fg3">
          <ChevronDown12Regular />
        </SelectPrimitive.Icon>
      </SelectPrimitive.Trigger>
      <SelectPrimitive.Portal>
        <SelectPrimitive.Content
          data-slot="select-content"
          position="popper"
          side="bottom"
          align="start"
          sideOffset={2}
          collisionPadding={8}
          className={cn(
            'z-50 box-border max-h-(--radix-select-content-available-height) min-w-(--radix-select-trigger-width) overflow-hidden rounded-md border border-transparent bg-bg1 text-300 leading-300 text-fg1 shadow-16',
            'data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[side=bottom]:slide-in-from-top-2 data-[side=top]:slide-in-from-bottom-2',
          )}
        >
          <SelectPrimitive.Viewport className="flex flex-col gap-0.5 p-1">
            {options.map((o) => (
              <SelectPrimitive.Item
                key={o.value}
                value={o.value === '' ? EMPTY : o.value}
                className={cn(
                  'relative box-border flex min-h-8 cursor-pointer items-center gap-1 rounded-md py-1.5 pr-2.5 pl-1.5 text-fg1 outline-none select-none',
                  'data-highlighted:bg-(--colorNeutralBackground1Hover) active:bg-(--colorNeutralBackground1Pressed)',
                )}
              >
                {/* Fluent keeps the checkmark column even on unselected options. */}
                <span className="inline-flex size-4 shrink-0 items-center justify-center text-[16px] leading-0 text-(--colorCompoundBrandForeground1)">
                  <SelectPrimitive.ItemIndicator>
                    <CheckmarkFilled />
                  </SelectPrimitive.ItemIndicator>
                </span>
                <SelectPrimitive.ItemText>{o.label}</SelectPrimitive.ItemText>
              </SelectPrimitive.Item>
            ))}
          </SelectPrimitive.Viewport>
        </SelectPrimitive.Content>
      </SelectPrimitive.Portal>
    </SelectPrimitive.Root>
  )
}

export { Select, type SelectOption }
