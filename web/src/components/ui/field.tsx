import { DiamondDismiss12Filled } from '@fluentui/react-icons/headless/svg/diamond-dismiss'
import { cloneElement, useId, type ReactElement, type ReactNode } from 'react'
import { cn } from '@/lib/utils'

interface FieldProps {
  label: ReactNode
  /** Shown under the control; marks the field invalid. */
  validationMessage?: ReactNode
  /** One form control (an Input); it receives the description, the invalid state and an id unless it has one. */
  children: ReactElement<{ id?: string; 'aria-invalid'?: boolean; 'aria-describedby'?: string }>
  className?: string
}

/** Fluent 2 vertical field: label above the control, error message below. */
function Field({ label, validationMessage, children, className }: FieldProps) {
  const generatedId = useId()
  const id = children.props.id ?? generatedId
  const messageId = `${id}-message`
  const invalid = validationMessage != null
  return (
    <div data-slot="field" className={cn('grid', className)}>
      <label htmlFor={id} className="mb-0.5 max-h-max max-w-max py-0.5 text-300 leading-300 text-fg1">
        {label}
      </label>
      {cloneElement(children, {
        id,
        'aria-invalid': invalid || undefined,
        'aria-describedby': invalid ? messageId : undefined,
      })}
      {invalid && (
        <div id={messageId} role="alert" className="mt-0.5 pl-4 text-200 leading-200 text-(--colorPaletteRedForeground1)">
          <span className="mr-1 -ml-4 inline-block align-[-1px] text-[12px] leading-0">
            <DiamondDismiss12Filled />
          </span>
          {validationMessage}
        </div>
      )}
    </div>
  )
}

export { Field }
