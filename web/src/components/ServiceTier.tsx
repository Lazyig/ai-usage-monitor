import { Badge } from './ui/badge'
import { RowField } from './RowField'
import { Select } from './ui/select'

export function serviceTierLabel(tier?: string): string {
  if (!tier || tier === 'unknown') return '알 수 없음'
  return ({ default: 'Standard', fast: 'Fast', priority: 'Fast', ultrafast: 'Ultrafast', flex: 'Flex', auto: '자동' } as Record<string, string>)[tier] ?? tier
}

export function ServiceTierBadge({ tier }: { tier?: string }) {
  return <Badge color={tier === 'fast' || tier === 'priority' ? 'brand' : 'informative'} title="선택한 서비스 등급이며 실제 처리·청구 등급과 다를 수 있습니다.">{serviceTierLabel(tier)}</Badge>
}

export function ServiceTierFilter({ id, value, onChange }: { id: string; value: string; onChange: (value: string) => void }) {
  return (
    <RowField label="서비스 등급 (선택 설정)">
      <Select id={id} value={value} onValueChange={onChange} options={[
        { value: '', label: '전체' },
        ...['default', 'fast', 'ultrafast', 'flex', 'auto', 'unknown'].map((tier) => ({ value: tier, label: serviceTierLabel(tier) })),
      ]} />
    </RowField>
  )
}
