import { Checkmark20Regular } from '@fluentui/react-icons/headless/svg/checkmark'
import { DropdownMenu as MenuPrimitive } from 'radix-ui'
import type { ComponentProps, ReactNode } from 'react'
import { cn } from '@/lib/utils'

/** Non-modal like Fluent's menu: the page stays hoverable and scrollable while it is open. */
function DropdownMenu(props: ComponentProps<typeof MenuPrimitive.Root>) {
  return <MenuPrimitive.Root modal={false} {...props} />
}

const DropdownMenuTrigger = MenuPrimitive.Trigger

/** Fluent 2 menu popover: a 4px-padded surface with a 16dp shadow. */
function DropdownMenuContent({ className, sideOffset = 0, onCloseAutoFocus, ...props }: ComponentProps<typeof MenuPrimitive.Content>) {
  return (
    <MenuPrimitive.Portal>
      <MenuPrimitive.Content
        data-slot="dropdown-menu-content"
        sideOffset={sideOffset}
        onCloseAutoFocus={(event) => {
          onCloseAutoFocus?.(event)
          // An item that opened a dialog hands focus to it: sending focus back to
          // the trigger would leave the dialog's focus trap with nothing focused.
          const dialog = document.querySelector<HTMLElement>('[role=dialog]')
          if (dialog && !event.defaultPrevented) {
            event.preventDefault()
            ;(dialog.querySelector<HTMLElement>('input, button') ?? dialog).focus()
          }
        }}
        className={cn(
          'z-50 box-border flex w-max max-w-[300px] min-w-[138px] flex-col gap-0.5 overflow-x-hidden overflow-y-auto rounded-md border border-transparent bg-bg1 p-1 text-300 leading-300 text-fg1 shadow-16',
          'data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[side=bottom]:slide-in-from-top-2 data-[side=top]:slide-in-from-bottom-2',
          className,
        )}
        {...props}
      />
    </MenuPrimitive.Portal>
  )
}

interface DropdownMenuItemProps extends ComponentProps<typeof MenuPrimitive.Item> {
  icon?: ReactNode
  /** Destructive entries are drawn in the danger colour. */
  danger?: boolean
}

function DropdownMenuItem({ className, icon, danger, children, ...props }: DropdownMenuItemProps) {
  return (
    <MenuPrimitive.Item
      data-slot="dropdown-menu-item"
      className={cn(
        'relative box-border flex max-w-[290px] min-h-8 shrink-0 cursor-pointer items-start gap-1 rounded-md bg-bg1 p-1.5 text-300 text-fg2 outline-none select-none',
        'data-highlighted:bg-(--colorNeutralBackground1Hover) data-highlighted:text-(--colorNeutralForeground2Hover) data-highlighted:[&_[data-slot=menu-item-icon]]:text-(--colorNeutralForeground2BrandSelected) active:bg-(--colorNeutralBackground1Pressed) active:text-(--colorNeutralForeground2Pressed)',
        'focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-(--colorStrokeFocus2)',
        'data-disabled:cursor-not-allowed data-disabled:bg-bg1 data-disabled:text-(--colorNeutralForegroundDisabled)',
        danger && 'not-data-disabled:text-danger not-data-disabled:data-highlighted:text-danger not-data-disabled:data-highlighted:[&_[data-slot=menu-item-icon]]:text-danger',
        className,
      )}
      {...props}
    >
      {icon != null && (
        <span data-slot="menu-item-icon" className="inline-flex size-5 shrink-0 items-center justify-center text-[20px] leading-0">
          {icon}
        </span>
      )}
      <span className="grow bg-transparent px-0.5">{children}</span>
    </MenuPrimitive.Item>
  )
}

interface DropdownMenuCheckItemProps extends Omit<ComponentProps<typeof MenuPrimitive.CheckboxItem>, 'onCheckedChange'> {
  checked: boolean
}

/** A menu entry with an on/off state (menuitemcheckbox), shown by a check mark
 * in the icon column. onSelect runs as for a plain item. */
function DropdownMenuCheckItem({ className, checked, children, ...props }: DropdownMenuCheckItemProps) {
  return (
    <MenuPrimitive.CheckboxItem
      data-slot="dropdown-menu-item"
      checked={checked}
      className={cn(
        'relative box-border flex max-w-[290px] min-h-8 shrink-0 cursor-pointer items-start gap-1 rounded-md bg-bg1 p-1.5 text-300 text-fg2 outline-none select-none',
        'data-highlighted:bg-(--colorNeutralBackground1Hover) data-highlighted:text-(--colorNeutralForeground2Hover) active:bg-(--colorNeutralBackground1Pressed) active:text-(--colorNeutralForeground2Pressed)',
        'focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-(--colorStrokeFocus2)',
        className,
      )}
      {...props}
    >
      <span data-slot="menu-item-icon" className="inline-flex size-5 shrink-0 items-center justify-center text-[20px] leading-0">
        <MenuPrimitive.ItemIndicator>
          <Checkmark20Regular />
        </MenuPrimitive.ItemIndicator>
      </span>
      <span className="grow bg-transparent px-0.5">{children}</span>
    </MenuPrimitive.CheckboxItem>
  )
}

function DropdownMenuSeparator({ className, ...props }: ComponentProps<typeof MenuPrimitive.Separator>) {
  return <MenuPrimitive.Separator data-slot="dropdown-menu-separator" className={cn('mx-[-5px] my-1 border-b border-stroke2', className)} {...props} />
}

export { DropdownMenu, DropdownMenuCheckItem, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger }
