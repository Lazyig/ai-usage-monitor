import { Tabs as TabsPrimitive } from 'radix-ui'
import type { ComponentProps, ReactNode } from 'react'
import { cn } from '@/lib/utils'

interface TabListProps extends Omit<ComponentProps<typeof TabsPrimitive.List>, 'dir'> {
  value: string
  onValueChange: (value: string) => void
}

/**
 * Fluent 2 small subtle-circular tab list, used as a selector without panels.
 * Arrow keys move focus; Enter or Space selects (each selection may fetch).
 */
function TabList({ value, onValueChange, className, children, ...props }: TabListProps) {
  return (
    <TabsPrimitive.Root value={value} onValueChange={onValueChange} activationMode="manual">
      <TabsPrimitive.List data-slot="tab-list" className={cn('relative flex shrink-0 flex-row items-stretch gap-1.5', className)} {...props}>
        {children}
      </TabsPrimitive.List>
    </TabsPrimitive.Root>
  )
}

/** Fluent 2 subtle-circular tab look, shared by Tab and TabButton. */
const TAB_CLASS = [
  'relative grid shrink-0 cursor-pointer grid-flow-col items-center justify-center overflow-hidden rounded-full border border-transparent bg-transparent px-1.5 py-px text-300 leading-300 text-fg2 outline-none',
  'enabled:hover:border-(--colorNeutralStroke1Hover) enabled:hover:bg-(--colorSubtleBackgroundHover) enabled:hover:text-(--colorNeutralForeground2Hover) enabled:active:border-(--colorNeutralStroke1Pressed) enabled:active:bg-(--colorSubtleBackgroundPressed) enabled:active:text-(--colorNeutralForeground2Pressed)',
  'data-[state=active]:border-(--colorCompoundBrandStroke) data-[state=active]:bg-(--colorBrandBackground2) data-[state=active]:font-semibold data-[state=active]:text-(--colorBrandForeground2) data-[state=active]:enabled:hover:border-(--colorCompoundBrandStrokeHover) data-[state=active]:enabled:hover:bg-(--colorBrandBackground2Hover) data-[state=active]:enabled:hover:text-(--colorBrandForeground2Hover) data-[state=active]:enabled:active:border-(--colorCompoundBrandStrokePressed) data-[state=active]:enabled:active:bg-(--colorBrandBackground2Pressed) data-[state=active]:enabled:active:text-(--colorBrandForeground2Pressed)',
  'disabled:cursor-not-allowed disabled:text-(--colorNeutralForegroundDisabled)',
  'focus-visible:z-1 focus-visible:border-transparent focus-visible:shadow-[var(--shadow4),0_0_0_2px_var(--colorStrokeFocus2),inset_0_0_0_1px_var(--colorNeutralStrokeOnBrand)]',
].join(' ')

function Tab({ className, children, ...props }: ComponentProps<typeof TabsPrimitive.Trigger>) {
  return (
    <TabsPrimitive.Trigger
      data-slot="tab"
      // There are no tab panels, so do not point at one that does not exist.
      aria-controls={undefined}
      className={cn(TAB_CLASS, className)}
      {...props}
    >
      <TabLabel>{children}</TabLabel>
    </TabsPrimitive.Trigger>
  )
}

/** Both labels in one grid cell: the bold copy keeps the width stable when
 * selection makes the label semibold. */
function TabLabel({ children }: { children: ReactNode }) {
  return (
    <>
      <span className="col-start-1 row-start-1 overflow-hidden px-0.5">{children}</span>
      <span aria-hidden className="invisible col-start-1 row-start-1 overflow-hidden px-0.5 font-semibold">
        {children}
      </span>
    </>
  )
}

interface TabButtonProps extends ComponentProps<'button'> {
  pressed: boolean
}

/**
 * A toggle button drawn like a Tab, for a selector whose entries come with
 * their own menus (a tablist may hold nothing but tabs). It announces its
 * state with aria-pressed and is reached with Tab like any button.
 */
function TabButton({ pressed, className, children, ...props }: TabButtonProps) {
  return (
    <button type="button" aria-pressed={pressed} data-state={pressed ? 'active' : 'inactive'} className={cn(TAB_CLASS, className)} {...props}>
      <TabLabel>{children}</TabLabel>
    </button>
  )
}

export { Tab, TabButton, TabList }
