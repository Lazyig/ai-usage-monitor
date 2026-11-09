import { RadioGroup } from 'radix-ui'
import { Card } from '../components/Card'
import { PageHeader } from '../components/PageHeader'
import { formatCompactIn, formatFull, isNumberUnits, type NumberUnits } from '../lib/format'
import { useFilters } from '../state/FiltersContext'

interface UnitOption {
  value: NumberUnits
  label: string
  /** The unit symbols, smallest first. */
  units: string
}

const UNIT_OPTIONS: UnitOption[] = [
  { value: 'en', label: '영어 단위', units: 'K · M · B' },
  { value: 'ko', label: '한국어 단위', units: '만 · 억 · 조' },
]

/** One value per unit step, so each option shows every symbol it uses. */
const SAMPLES = [8_420, 356_000, 47_800_000, 2_150_000_000]

// Fluent 2 selectable tile: a neutral stroke at rest, the brand stroke (doubled
// with an inset ring, so the width does not shift) once chosen.
const TILE =
  'group flex min-w-0 cursor-pointer flex-col gap-3 rounded-lg border border-(--colorNeutralStroke1) bg-bg1 p-4 text-left text-fg1 transition-[border-color,box-shadow] duration-(--durationFaster) ease-(--curveLinear) hover:border-(--colorNeutralStroke1Hover) focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-(--colorStrokeFocus2) data-[state=checked]:border-(--colorCompoundBrandStroke) data-[state=checked]:shadow-[inset_0_0_0_1px_var(--colorCompoundBrandStroke)]'

function UnitTile({ option }: { option: UnitOption }) {
  return (
    // A button holds phrasing content only, so the samples are spans; the
    // name stays the label and its symbols rather than every sample.
    <RadioGroup.Item value={option.value} aria-label={`${option.label} (${option.units})`} className={TILE}>
      <span className="flex items-center gap-2.5">
        {/* Fluent radio dot: ring at rest, brand ring and dot when chosen. */}
        <span
          aria-hidden="true"
          className="grid size-4 flex-none place-items-center rounded-full border border-(--colorNeutralStrokeAccessible) group-data-[state=checked]:border-(--colorCompoundBrandStroke)"
        >
          <span className="size-2 rounded-full bg-(--colorCompoundBrandForeground1) opacity-0 group-data-[state=checked]:opacity-100" />
        </span>
        <span className="text-300 leading-300 font-semibold">{option.label}</span>
        <span className="ms-auto text-200 leading-200 text-fg3">{option.units}</span>
      </span>
      <span aria-hidden="true" className="grid grid-cols-[1fr_auto] gap-x-4 gap-y-1 ps-[26px] text-200 leading-200 tabular-nums">
        {SAMPLES.map((n) => (
          <span key={n} className="contents">
            <span className="text-fg3">{formatFull(n)}</span>
            <span className="text-end font-semibold text-fg1">{formatCompactIn(n, option.value)}</span>
          </span>
        ))}
      </span>
    </RadioGroup.Item>
  )
}

/** Display preferences. Each one is saved on the server like the period, so
 * it follows the operator to another browser. */
export function Settings() {
  const { numberUnits, setNumberUnits } = useFilters()

  return (
    <div>
      <PageHeader title="설정" />
      <Card title="숫자 단위" subtitle="토큰 수처럼 큰 숫자를 줄여 쓰는 방식입니다. 정확한 값은 숫자에 마우스를 올리면 보입니다." className="max-w-[720px]">
        <RadioGroup.Root
          aria-label="숫자 단위"
          value={numberUnits}
          onValueChange={(v) => isNumberUnits(v) && setNumberUnits(v)}
          className="grid grid-cols-2 gap-3 max-phone:grid-cols-1"
        >
          {UNIT_OPTIONS.map((o) => (
            <UnitTile key={o.value} option={o} />
          ))}
        </RadioGroup.Root>
        <p className="m-0 text-200 leading-200 text-fg3">고르면 모든 화면에 바로 적용되고 서버에 저장되어 다른 브라우저에서도 이어집니다.</p>
      </Card>
    </div>
  )
}
