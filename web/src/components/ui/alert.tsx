import { DiamondDismissFilled } from '@fluentui/react-icons/headless/svg/diamond-dismiss'
import type { ComponentProps, ReactNode } from 'react'
import { cn } from '@/lib/utils'

interface AlertProps extends ComponentProps<'div'> {
  /** Trailing controls such as a retry button. */
  actions?: ReactNode
}

/** Fluent 2 error message bar: icon, message and actions on a tinted strip. */
function Alert({ className, actions, children, ...props }: AlertProps) {
  return (
    <div
      data-slot="alert"
      role="alert"
      className={cn(
        'box-border flex min-h-9 flex-wrap items-center rounded-md border border-(--colorStatusDangerBorder1) bg-(--colorStatusDangerBackground1) py-1.5 pl-3',
        className,
      )}
      {...props}
    >
      <span className="mr-2 flex items-center text-500 text-danger">
        <DiamondDismissFilled />
      </span>
      <div className="min-w-0 flex-1 pr-3 text-300 leading-300">{children}</div>
      {actions != null && <div className="flex gap-x-3 pr-3">{actions}</div>}
    </div>
  )
}

export { Alert }
