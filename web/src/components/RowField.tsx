import type { ComponentProps } from 'react'
import { Field } from './ui/field'

/**
 * A labelled control in a horizontal form or filter row: tighter than a dialog
 * field, at least 180px wide, and full-width and touch-height on phones, where
 * the row stacks.
 */
export function RowField(props: Omit<ComponentProps<typeof Field>, 'className'>) {
  return (
    <Field
      className="max-phone:w-full [&>label]:mb-1 [&>label]:py-0 [&_input]:px-2.5 [&>[data-slot=input]]:min-w-[180px] [&>[data-slot=native-select]]:min-w-[180px] max-phone:[&>[data-slot=input]]:min-h-10 max-phone:[&>[data-slot=native-select]]:h-10"
      {...props}
    />
  )
}
