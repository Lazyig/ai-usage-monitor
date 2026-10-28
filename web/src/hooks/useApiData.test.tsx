// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, expect, it } from 'vitest'
import { useApiData } from './useApiData'

let root: Root
let container: HTMLDivElement
let state: ReturnType<typeof useApiData<string>>
const pending: ((v: string) => void)[] = []

function Probe({ source }: { source: string }) {
  state = useApiData(() => new Promise<string>((resolve) => pending.push(resolve)), [source])
  return null
}

beforeEach(() => {
  ;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true
  container = document.createElement('div')
  root = createRoot(container)
  pending.length = 0
})

afterEach(() => {
  act(() => root.unmount())
})

it('keeps the data through a refresh but not through a filter change', async () => {
  act(() => root.render(<Probe source="all" />))
  await act(async () => pending[0]('all-data'))
  expect(state).toMatchObject({ status: 'success', data: 'all-data' })

  // A refresh asks the same question again: the old answer stays up meanwhile.
  act(() => state.reload())
  expect(state).toMatchObject({ status: 'success', data: 'all-data' })
  await act(async () => pending[1]('all-data-2'))

  // Another filter asks something else: the all-sources numbers must not be
  // shown as if they were the codex ones.
  act(() => root.render(<Probe source="codex" />))
  expect(state.status).toBe('loading')
  await act(async () => pending[2]('codex-data'))
  expect(state).toMatchObject({ status: 'success', data: 'codex-data' })
})
