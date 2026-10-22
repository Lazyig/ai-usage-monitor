import { Tab, TabList } from '@fluentui/react-components'

export interface SegmentedOption<T extends string> {
  value: T
  label: string
  /** Greyed out and unselectable — pair it with `title` to say why. */
  disabled?: boolean
  title?: string
}

interface SegmentedControlProps<T extends string> {
  value: T
  onChange: (value: T) => void
  options: SegmentedOption<T>[]
  ariaLabel: string
}

/**
 * Shared metric switch (cost/tokens/…) as a Fluent circular tab list, so every
 * metric switch (Overview chart, Models charts, Models trend, …) looks
 * identical instead of each page rolling its own button group.
 */
export function SegmentedControl<T extends string>({ value, onChange, options, ariaLabel }: SegmentedControlProps<T>) {
  return (
    <TabList
      className="flex-wrap"
      size="small"
      appearance="subtle-circular"
      aria-label={ariaLabel}
      selectedValue={value}
      onTabSelect={(_, data) => onChange(data.value as T)}
    >
      {options.map((opt) => (
        <Tab key={opt.value} value={opt.value} disabled={opt.disabled} title={opt.title}>
          {opt.label}
        </Tab>
      ))}
    </TabList>
  )
}
