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
 * Shared pill-style toggle group for metric switches (cost/tokens/…). One
 * definition of height, radius and active style so every metric switch
 * (Overview chart, Models charts, Models trend, …) looks identical instead
 * of each page rolling its own button group.
 */
export function SegmentedControl<T extends string>({ value, onChange, options, ariaLabel }: SegmentedControlProps<T>) {
  return (
    <div className="chart-toggle" role="group" aria-label={ariaLabel}>
      {options.map((opt) => (
        <button
          key={opt.value}
          type="button"
          className={`chip${value === opt.value ? ' chip-active' : ''}`}
          aria-pressed={value === opt.value}
          disabled={opt.disabled}
          title={opt.title}
          onClick={() => onChange(opt.value)}
        >
          {opt.label}
        </button>
      ))}
    </div>
  )
}
