// Source × device matrix: the shared data shape behind the composition charts
// on the 에이전트 and 기기 pages. The API has no two-dimensional grouping, so
// this fans out one `/breakdown?group_by=device&source=…` request per source
// and pivots the results.
import { getBreakdown } from '../api/client'
import type { BreakdownRow, Source } from '../api/types'
import { tokensOf } from './usage'

interface MatrixEntry {
  source: Source
  rows: BreakdownRow[]
}

/** One (source, device) intersection. `cost` sums only the rows with a known
 * price; `unpriced` marks that at least one merged row had none. */
interface MatrixCell {
  cost: number
  tokens: number
  events: number
  unpriced: boolean
}

interface MatrixDevice extends MatrixCell {
  id: string
  name: string
  /** Position in the id-sorted device list — the color slot. Fixed per device
   * so a filter (which changes the visible set and its order) never repaints
   * the survivors. */
  colorIndex: number
  /** Set on the synthetic bucket that folds the long tail. */
  other?: boolean
}

interface MatrixSource extends MatrixCell {
  key: Source
}

interface Matrix {
  /** Devices with data, largest first; anything past `maxDevices` is folded
   * into a single "기타" row. */
  devices: MatrixDevice[]
  sources: MatrixSource[]
  cell: (source: Source, deviceId: string) => MatrixCell
  total: MatrixCell
}

const OTHER_DEVICE_ID = '__other__'

/** Past this the stacked segments stop being distinguishable, so the tail
 * folds into one neutral band instead of cycling hues. */
const MAX_DEVICES = 7

function emptyCell(): MatrixCell {
  return { cost: 0, tokens: 0, events: 0, unpriced: false }
}

function addRow(target: MatrixCell, row: BreakdownRow): void {
  target.tokens += tokensOf(row)
  target.events += row.events
  if (row.cost_usd === null) target.unpriced = true
  else target.cost += row.cost_usd
}

function addCell(target: MatrixCell, other: MatrixCell): void {
  target.cost += other.cost
  target.tokens += other.tokens
  target.events += other.events
  target.unpriced = target.unpriced || other.unpriced
}

/** Fetches one device breakdown per source; used with `useApiData` by both pages. */
export function fetchSourceDeviceMatrix(params: { from: string; to: string; sources: Source[] }): Promise<MatrixEntry[]> {
  return Promise.all(
    params.sources.map(async (source) => ({
      source,
      rows: (await getBreakdown({ from: params.from, to: params.to, group_by: 'device', source, limit: 200 })).rows,
    })),
  )
}

/**
 * Pivots the per-source breakdowns into a device × source matrix.
 * Row keys are device ids; `deviceNames` (from `GET /devices`) only fills in
 * for rows whose `name` the server left out.
 */
export function buildMatrix(entries: MatrixEntry[], deviceNames: Map<string, string> = new Map(), maxDevices = MAX_DEVICES): Matrix {
  const cells = new Map<string, MatrixCell>()
  const devices = new Map<string, MatrixDevice>()
  const sources = new Map<Source, MatrixSource>()
  const total = emptyCell()

  for (const entry of entries) {
    const sourceAgg: MatrixSource = sources.get(entry.source) ?? { key: entry.source, ...emptyCell() }
    for (const row of entry.rows) {
      const id = row.key
      const dev: MatrixDevice = devices.get(id) ?? {
        id,
        name: row.name ?? deviceNames.get(id) ?? id,
        colorIndex: 0,
        ...emptyCell(),
      }
      const cell = cells.get(cellKey(entry.source, id)) ?? emptyCell()

      addRow(cell, row)
      addRow(dev, row)
      addRow(sourceAgg, row)
      addRow(total, row)

      cells.set(cellKey(entry.source, id), cell)
      devices.set(id, dev)
    }
    sources.set(entry.source, sourceAgg)
  }

  // Color slots come from the id-sorted order, never from the usage ranking.
  for (const [i, id] of [...devices.keys()].sort().entries()) devices.get(id)!.colorIndex = i

  const ranked = [...devices.values()].sort((a, b) => b.tokens - a.tokens || a.name.localeCompare(b.name))
  const folded = new Set<string>()
  let shown = ranked
  if (ranked.length > maxDevices) {
    const tail = ranked.slice(maxDevices - 1)
    const other: MatrixDevice = { id: OTHER_DEVICE_ID, name: `기타 ${tail.length}대`, colorIndex: -1, other: true, ...emptyCell() }
    for (const d of tail) {
      folded.add(d.id)
      addCell(other, d)
    }
    shown = [...ranked.slice(0, maxDevices - 1), other]
  }

  return {
    devices: shown,
    sources: [...sources.values()],
    total,
    cell: (source, deviceId) => {
      if (deviceId !== OTHER_DEVICE_ID) return cells.get(cellKey(source, deviceId)) ?? emptyCell()
      const agg = emptyCell()
      for (const id of folded) {
        const c = cells.get(cellKey(source, id))
        if (c) addCell(agg, c)
      }
      return agg
    },
  }
}

function cellKey(source: Source, deviceId: string): string {
  return `${source}|${deviceId}`
}
