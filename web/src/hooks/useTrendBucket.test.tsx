// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, expect, it } from 'vitest'
import { type DateRange, rangeForPreset } from '../lib/dateRange'
import { useTrendBucket } from './useTrendBucket'

let root: Root
let state: ReturnType<typeof useTrendBucket>

function Probe({ range }: { range: DateRange }) {
  state = useTrendBucket(range)
  return null
}

beforeEach(() => {
  ;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true
  root = createRoot(document.createElement('div'))
})

afterEach(() => {
  act(() => root.unmount())
})

it('follows the range until a pick, and drops the pick when the range changes', () => {
  const quarter = rangeForPreset('last-90')
  act(() => root.render(<Probe range={quarter} />))
  expect(state.resolve().bucket).toBe('day')

  act(() => state.select('week'))
  expect(state.resolve().bucket).toBe('week')
  expect(state.fetchBucket).toBe('day')

  act(() => root.render(<Probe range={rangeForPreset('day-0')} />))
  expect(state.resolve().bucket).toBe('hour')
  expect(state.fetchBucket).toBe('hour')

  act(() => root.render(<Probe range={quarter} />))
  expect(state.resolve().bucket).toBe('day')
})

it('fetches days for a pick that is folded from them', () => {
  act(() => root.render(<Probe range={rangeForPreset('day-0')} />))
  act(() => state.select('day'))
  expect(state.fetchBucket).toBe('day')
  expect(state.resolve().bucket).toBe('day')
})
