// Per-endpoint semantics for the mock backend used when VITE_MOCK=1. Mirrors
// docs/API.md shapes exactly so every page can be visually sanity-checked
// without a running Go server. The data these read from lives in ./data.ts.
import type {
  BreakdownResponse,
  BreakdownRow,
  CreateDeviceResponse,
  DeleteDeviceResponse,
  Device,
  DownloadsManifest,
  EventsResponse,
  GroupBy,
  HealthResponse,
  PricingRate,
  PricingResponse,
  PurgeDeviceEventsResponse,
  PurgeDeviceResponse,
  ResyncDeviceResponse,
  RotateDeviceResponse,
  Source,
  SummaryResponse,
  SummaryRowBase,
  ThroughputGroup,
  ThroughputResponse,
  TimeseriesResponse,
  TimeseriesSeries,
  UsageTotals,
} from '../types'
import {
  type DayRecord,
  MODELS,
  TODAY,
  addDays,
  dataset,
  flatEvents,
  hashStr,
  listDevices,
  purgeDeviceRecords,
  rngFor,
  sessionKey,
  setDevices,
} from './data'

// Debug-only toggle (see web/README.md verification notes): set
// VITE_MOCK_FAIL=summary to make GET /api/v1/summary fail with a 500,
// so the per-widget error rendering on the Overview page can be eyeballed
// without a real backend.
const MOCK_FAIL = import.meta.env.VITE_MOCK_FAIL as string | undefined

type Params = Record<string, string | undefined>

// ---- request helpers ---------------------------------------------------

function inRange(date: string, from: string, to: string): boolean {
  return date >= from && date < to
}

function normalizeRange(params: Params): { from: string; to: string } {
  const to = params.to ? params.to.slice(0, 10) : addDays(TODAY, 1)
  const from = params.from ? params.from.slice(0, 10) : addDays(to, -30)
  return { from, to }
}

/** The records in range, narrowed by the `source`/`device` filters every
 * aggregate endpoint accepts. */
function selectRecords(params: Params): { from: string; to: string; rows: DayRecord[] } {
  const { from, to } = normalizeRange(params)
  let rows = dataset().filter((r) => inRange(r.date, from, to))
  if (params.source) rows = rows.filter((r) => r.source === params.source)
  if (params.device) rows = rows.filter((r) => r.deviceId === params.device)
  return { from, to, rows }
}

interface Accumulator {
  input: number
  output: number
  cache_read: number
  cache_write: number
  reasoning?: number
  cost_usd: number | null
  events: number
}

function emptyTotals(): SummaryRowBase {
  return { key: '', input: 0, output: 0, cache_read: 0, cache_write: 0, reasoning: 0, cost_usd: 0, events: 0 }
}

function mergeRow(target: Accumulator, rec: DayRecord) {
  target.input += rec.input
  target.output += rec.output
  target.cache_read += rec.cache_read
  target.cache_write += rec.cache_write
  target.reasoning = (target.reasoning ?? 0) + rec.reasoning
  target.events += rec.events
  if (rec.cost_usd !== null) {
    target.cost_usd = (target.cost_usd ?? 0) + rec.cost_usd
  }
}

function recordTokens(r: DayRecord): number {
  return r.input + r.output + r.cache_read + r.cache_write
}

// ---- GET /api/v1/summary -------------------------------------------------

function buildSummary(params: Params): SummaryResponse {
  const { from, to, rows } = selectRecords(params)

  const totals: UsageTotals = { input: 0, output: 0, cache_read: 0, cache_write: 0, reasoning: 0, cost_usd: 0, cost_known_ratio: 0, events: 0 }
  let knownTokens = 0
  let totalTokens = 0
  let anyKnownCost = false
  const bySource = new Map<string, SummaryRowBase>()
  const byDevice = new Map<string, SummaryRowBase & { name: string }>()
  const byModel = new Map<string, SummaryRowBase & { source: Source; estimated: boolean }>()

  for (const r of rows) {
    mergeRow(totals, r)
    const tokens = recordTokens(r)
    totalTokens += tokens
    if (r.cost_usd !== null) {
      knownTokens += tokens
      anyKnownCost = true
    }

    const s = bySource.get(r.source) ?? { ...emptyTotals(), key: r.source, cost_usd: null }
    mergeRow(s, r)
    bySource.set(r.source, s)

    const d = byDevice.get(r.deviceId) ?? { ...emptyTotals(), key: r.deviceId, name: r.deviceName, cost_usd: null }
    mergeRow(d, r)
    byDevice.set(r.deviceId, d)

    const mKey = `${r.source}:${r.model}`
    const m = byModel.get(mKey) ?? { ...emptyTotals(), key: r.model, source: r.source, estimated: r.estimated, cost_usd: null }
    mergeRow(m, r)
    byModel.set(mKey, m)
  }

  totals.cost_known_ratio = totalTokens === 0 ? 1 : knownTokens / totalTokens
  if (!anyKnownCost) totals.cost_usd = null

  const byCostDesc = <T extends { cost_usd: number | null }>(a: T, b: T) => (b.cost_usd ?? 0) - (a.cost_usd ?? 0)
  return {
    from: `${from}T00:00:00Z`,
    to: `${to}T00:00:00Z`,
    totals,
    by_source: [...bySource.values()].sort(byCostDesc),
    by_device: [...byDevice.values()].sort(byCostDesc),
    by_model: [...byModel.values()].sort(byCostDesc),
  }
}

// ---- GET /api/v1/timeseries ---------------------------------------------

interface BucketPoint {
  input: number
  output: number
  cache_read: number
  cache_write: number
  reasoning: number
  cost_usd: number | null
  events: number
}

function emptyPoint(): BucketPoint {
  return { input: 0, output: 0, cache_read: 0, cache_write: 0, reasoning: 0, cost_usd: null, events: 0 }
}

/** Adds `rec * frac` into a bucket. Day buckets take the whole record
 * (frac 1, no rounding); hour buckets take a rounded share of it. */
function addToPoint(p: BucketPoint, r: DayRecord, frac: number): void {
  const part = frac === 1 ? (v: number) => v : (v: number) => Math.round(v * frac)
  p.input += part(r.input)
  p.output += part(r.output)
  p.cache_read += part(r.cache_read)
  p.cache_write += part(r.cache_write)
  p.reasoning += part(r.reasoning)
  p.events += part(r.events)
  if (r.cost_usd !== null) p.cost_usd = (p.cost_usd ?? 0) + r.cost_usd * frac
}

function buildTimeseries(params: Params): TimeseriesResponse {
  const { rows } = selectRecords(params)
  const bucket = params.bucket === 'hour' ? 'hour' : 'day'
  const groupBy = (params.group_by as GroupBy) ?? 'source'

  // Like the server, group_by=device keys by device UUID and carries the
  // human-readable name alongside it; every other grouping has no `name`.
  const keyOf = (r: DayRecord): string => {
    switch (groupBy) {
      case 'device':
        return r.deviceId
      case 'model':
        return r.model
      case 'project':
        return r.project
      case 'session':
        return sessionKey(r)
      default:
        return r.source
    }
  }
  const names = new Map<string, string>()
  const series = new Map<string, Map<string, BucketPoint>>()

  for (const r of rows) {
    const key = keyOf(r)
    if (!series.has(key)) series.set(key, new Map())
    if (groupBy === 'device') names.set(key, r.deviceName)
    const points = series.get(key)!

    if (bucket === 'day') {
      const p = points.get(r.date) ?? emptyPoint()
      addToPoint(p, r, 1)
      points.set(r.date, p)
    } else {
      // spread the day's total across a few representative hours
      const hrRng = rngFor('hr', r.date, r.deviceId, r.model)
      const hours = [9, 11, 14, 16, 20].filter(() => hrRng() > 0.3)
      const bucketHours = hours.length > 0 ? hours : [12]
      const frac = 1 / bucketHours.length
      for (const h of bucketHours) {
        const t = `${r.date}T${String(h).padStart(2, '0')}:00:00Z`
        const p = points.get(t) ?? emptyPoint()
        addToPoint(p, r, frac)
        points.set(t, p)
      }
    }
  }

  return {
    bucket,
    group_by: groupBy,
    series: [...series.entries()]
      .map(([key, points]): TimeseriesSeries => {
        const name = names.get(key)
        return {
          key,
          ...(name !== undefined ? { name } : {}),
          points: [...points.entries()]
            .sort(([a], [b]) => (a < b ? -1 : 1))
            .map(([t, v]) => ({ t, ...v })),
        }
      })
      .sort((a, b) => (a.name ?? a.key).localeCompare(b.name ?? b.key)),
  }
}

// ---- GET /api/v1/breakdown ----------------------------------------------

function buildBreakdown(params: Params): BreakdownResponse {
  const { rows } = selectRecords(params)
  const groupBy = (params.group_by as BreakdownResponse['group_by']) ?? 'model'
  const limit = params.limit ? parseInt(params.limit, 10) : 50

  // Like the server, group_by=device keys by device UUID and carries the
  // human-readable name in `name`; every other grouping leaves `name` unset.
  const keyOf = (r: DayRecord): string => {
    switch (groupBy) {
      case 'project':
        return r.project
      case 'device':
        return r.deviceId
      case 'session':
        return sessionKey(r)
      case 'source':
        return r.source
      default:
        return r.model
    }
  }

  // Rows merge many records, so `source`/`device`/`estimated` are only
  // meaningful when every merged record agrees — tracked here rather than
  // taken from whichever record happened to come first.
  interface RowMeta {
    sources: Set<Source>
    devices: Set<string>
    allEstimated: boolean
    knownTokens: number
    totalTokens: number
  }

  const acc = new Map<string, BreakdownRow>()
  const meta = new Map<string, RowMeta>()
  for (const r of rows) {
    const key = keyOf(r)
    const existing = acc.get(key)
    if (!existing) {
      acc.set(key, {
        key,
        ...(groupBy === 'device' ? { name: r.deviceName } : {}),
        input: r.input,
        output: r.output,
        cache_read: r.cache_read,
        cache_write: r.cache_write,
        reasoning: r.reasoning,
        cost_usd: r.cost_usd,
        estimated: r.estimated,
        events: r.events,
        first_seen: `${r.date}T00:00:00Z`,
        last_seen: `${r.date}T23:59:59Z`,
      })
      meta.set(key, { sources: new Set([r.source]), devices: new Set([r.deviceName]), allEstimated: r.estimated, knownTokens: 0, totalTokens: 0 })
    } else {
      existing.input += r.input
      existing.output += r.output
      existing.cache_read += r.cache_read
      existing.cache_write += r.cache_write
      existing.reasoning += r.reasoning
      existing.events += r.events
      if (r.cost_usd !== null) existing.cost_usd = (existing.cost_usd ?? 0) + r.cost_usd
      if (r.date < existing.first_seen) existing.first_seen = `${r.date}T00:00:00Z`
      if (r.date > existing.last_seen) existing.last_seen = `${r.date}T23:59:59Z`
      const m = meta.get(key)!
      m.sources.add(r.source)
      m.devices.add(r.deviceName)
      m.allEstimated = m.allEstimated && r.estimated
    }
    const m = meta.get(key)!
    const tokens = recordTokens(r)
    m.totalTokens += tokens
    if (r.cost_usd !== null) m.knownTokens += tokens
  }

  for (const [key, row] of acc) {
    const m = meta.get(key)!
    row.source = m.sources.size === 1 ? [...m.sources][0] : undefined
    row.device = m.devices.size === 1 ? [...m.devices][0] : undefined
    row.estimated = m.allEstimated
    row.cost_known_ratio = m.totalTokens === 0 ? 1 : m.knownTokens / m.totalTokens
  }

  const rowsOut = [...acc.values()].sort((a, b) => (b.cost_usd ?? 0) - (a.cost_usd ?? 0)).slice(0, limit)
  return { group_by: groupBy, rows: rowsOut }
}

// ---- GET /api/v1/throughput ---------------------------------------------

/** The mock dataset is per-day, so active minutes and peaks are synthesized
 * deterministically from each record: every event occupies roughly 1.5–3.5
 * minutes of wall clock, and the busiest minute runs 2–3× the average. */
function buildThroughput(params: Params): ThroughputResponse {
  const { rows } = selectRecords(params)
  const groupBy = (params.group_by as ThroughputResponse['group_by']) ?? 'source'
  const keyOf = (r: DayRecord): string => {
    switch (groupBy) {
      case 'project':
        return r.project
      case 'device':
        return r.deviceId
      case 'session':
        return sessionKey(r)
      case 'model':
        return r.model
      default:
        return r.source
    }
  }
  interface Acc {
    g: ThroughputGroup
    peakPromptScale: number
    peakOutputScale: number
    peakDate: string
  }
  const start = (key: string): Acc => ({
    g: {
      key,
      active_minutes: 0,
      prompt: 0,
      input: 0,
      cache_read: 0,
      cache_write: 0,
      output: 0,
      events: 0,
      per_minute: { prompt: 0, output: 0, total: 0 },
    },
    peakPromptScale: 2,
    peakOutputScale: 2,
    peakDate: '',
  })
  const fold = (a: Acc, r: DayRecord) => {
    const rng = rngFor('throughput', r.date, r.source, r.model, r.deviceId)
    a.g.active_minutes += Math.max(1, Math.round(r.events * (1.5 + rng() * 2)))
    a.g.input += r.input
    a.g.cache_read += r.cache_read
    a.g.cache_write += r.cache_write
    a.g.output += r.output
    a.g.events += r.events
    const scale = 2 + rng()
    if (scale > a.peakPromptScale) {
      a.peakPromptScale = scale
      a.peakDate = r.date
    }
    if (scale > a.peakOutputScale) a.peakOutputScale = scale
  }
  const finish = (a: Acc, name?: string): ThroughputGroup => {
    const g = a.g
    g.prompt = g.input + g.cache_read + g.cache_write
    if (g.active_minutes > 0) {
      g.per_minute = {
        prompt: Math.round((g.prompt / g.active_minutes) * 10) / 10,
        output: Math.round((g.output / g.active_minutes) * 10) / 10,
        total: Math.round(((g.prompt + g.output) / g.active_minutes) * 10) / 10,
      }
      const t = `${a.peakDate || TODAY}T14:07:00+09:00`
      g.peak_prompt = { t, tokens: Math.round(g.per_minute.prompt * a.peakPromptScale) }
      g.peak_output = { t, tokens: Math.round(g.per_minute.output * a.peakOutputScale) }
    }
    if (name) g.name = name
    return g
  }

  const accs = new Map<string, Acc>()
  const names = new Map<string, string>()
  const total = start('')
  for (const r of rows) {
    const key = keyOf(r)
    let a = accs.get(key)
    if (!a) {
      a = start(key)
      accs.set(key, a)
      if (groupBy === 'device') names.set(key, r.deviceName)
    }
    fold(a, r)
    fold(total, r)
  }
  // Groups overlap in time, so the union of their minutes is smaller than the sum.
  total.g.active_minutes = Math.round(total.g.active_minutes * 0.7)
  const groups = [...accs.entries()].map(([k, a]) => finish(a, names.get(k))).sort((x, y) => y.per_minute.total - x.per_minute.total)
  return { group_by: groupBy, tz: 'Asia/Seoul', total: finish(total), groups }
}

// ---- GET /api/v1/events --------------------------------------------------

function buildEvents(params: Params): EventsResponse {
  const { from, to } = normalizeRange(params)
  let rows = flatEvents().filter((e) => inRange(e.ts.slice(0, 10), from, to))
  if (params.source) rows = rows.filter((e) => e.source === params.source)
  if (params.device) rows = rows.filter((e) => e.device_id === params.device)
  if (params.model) rows = rows.filter((e) => e.model === params.model)

  const limit = params.limit ? parseInt(params.limit, 10) : 200
  const cursor = params.cursor ? parseInt(params.cursor, 10) : 0
  const page = rows.slice(cursor, cursor + limit)
  const nextCursor = cursor + limit < rows.length ? String(cursor + limit) : undefined

  return { events: page, next_cursor: nextCursor }
}

// ---- GET /api/v1/pricing -------------------------------------------------

// Real pricing.yaml rows come back with plenty of nulls (unpriced models),
// and `provider`/`alias_of`/`effective_from` are only present when the
// underlying rate actually has them — this must never assume any of those
// keys exist. See docs/API.md's implementation notes for the exact shape.
function buildPricing(): PricingResponse {
  const derived: PricingRate[] = Object.values(MODELS)
    .flat()
    .filter((m, i, arr) => arr.findIndex((x) => x.model === m.model) === i)
    .map((m) => {
      const p = m.priceUsdPer1M
      return {
        match: m.model,
        provider: m.model.startsWith('claude') ? 'anthropic' : m.model.startsWith('gpt') || m.model.includes('codex') ? 'openai' : 'google',
        input: p?.input ?? null,
        output: p?.output ?? null,
        cache_read: p?.cache_read ?? null,
        // Mirrors the real server: `cache_write` is only the generic/legacy
        // bucket — most current rates carry 5m/1h instead and leave this null.
        cache_write: null,
        cache_write_5m: p?.cache_write ?? null,
        cache_write_1h: p ? Math.round(p.cache_write * 1.6 * 100) / 100 : null,
        alias_of: m.model === 'gemini-default' ? 'gemini-3.7-flash' : undefined,
        effective_from: p ? '2026-06-01' : undefined,
        effective_to: p ? '2027-01-01' : undefined,
        note: p ? 'introductory pricing' : undefined,
        estimated: m.estimated,
      }
    })

  // A couple of rows lifted straight from the bug report's real-server
  // example, to guarantee this exact shape (nulls, a wildcard match,
  // missing provider/effective_from on the alias) is always exercised.
  const literal: PricingRate[] = [
    {
      match: 'claude-fable-5-1',
      provider: 'anthropic',
      input: 10,
      output: 50,
      cache_read: 0.25,
      cache_write: null,
      cache_write_5m: 12.5,
      cache_write_1h: 20,
      effective_from: '2026-08-01T00:00:00Z',
      estimated: false,
    },
    {
      match: 'gpt-5.6-*',
      provider: 'openai',
      input: null,
      output: null,
      cache_read: null,
      cache_write: null,
      cache_write_5m: null,
      cache_write_1h: null,
      estimated: false,
    },
  ]

  return { rates: [...literal, ...derived.filter((r) => r.match !== 'gpt-5.6-terra' && r.match !== 'gpt-5.6-sol')] }
}

// ---- GET /downloads/manifest.json, /health -------------------------------

function buildManifest(): DownloadsManifest {
  return {
    version: 'v0.3.1-mock',
    built_at: new Date(Date.now() - 3 * 86_400_000).toISOString(),
    files: [
      { name: 'ai-usage-agent_linux_amd64', os: 'linux', arch: 'amd64', size: 8_912_640, sha256: hashStr('linux_amd64').toString(16).padStart(64, '0') },
      { name: 'ai-usage-agent_darwin_arm64', os: 'darwin', arch: 'arm64', size: 8_654_320, sha256: hashStr('darwin_arm64').toString(16).padStart(64, '0') },
      { name: 'ai-usage-agent_windows_amd64.exe', os: 'windows', arch: 'amd64', size: 9_213_952, sha256: hashStr('windows_amd64').toString(16).padStart(64, '0') },
    ],
  }
}

function buildHealth(): HealthResponse {
  return { ok: true, version: 'v0.3.1-mock', events: flatEvents().length }
}

// ---- public mock dispatcher --------------------------------------------

export class MockApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

export interface MockRequestOptions {
  method?: string
  params?: Params
  body?: unknown
  token?: string | null
}

const DELAY_MS = 180

function delay<T>(value: T): Promise<T> {
  return new Promise((resolve) => setTimeout(() => resolve(value), DELAY_MS))
}

/** Looks a device up by id, 404-ing exactly as the server does. */
function requireDevice(id: string): Device {
  const existing = listDevices().find((d) => d.id === id)
  if (!existing) throw new MockApiError(404, 'device not found')
  return existing
}

function updateDevice(id: string, patch: (d: Device) => Device): void {
  setDevices(listDevices().map((d) => (d.id === id ? patch(d) : d)))
}

export async function mockRequest<T>(path: string, opts: MockRequestOptions): Promise<T> {
  const method = opts.method ?? 'GET'
  const params = opts.params ?? {}
  const token = opts.token

  // GET /api/v1/devices doubles as the login-verification call.
  if (path === '/api/v1/devices' && method === 'GET') {
    if (!token || token === 'invalid' || token === 'bad') {
      throw new MockApiError(401, 'unauthorized')
    }
    return delay({ devices: listDevices() } as unknown as T)
  }

  if (!token) throw new MockApiError(401, 'unauthorized')

  if (path === '/api/v1/devices' && method === 'POST') {
    const body = opts.body as { name: string }
    const id = crypto.randomUUID()
    const newDevice: Device = {
      id,
      name: body.name,
      os: 'unknown',
      arch: 'unknown',
      agent_version: '-',
      last_seen: '',
      last_ingest: '',
      events_total: 0,
      token_created_at: new Date().toISOString(),
    }
    setDevices([...listDevices(), newDevice])
    const resp: CreateDeviceResponse = { id, name: body.name, token: `aum_${hashStr(id).toString(16)}${Date.now().toString(16)}` }
    return delay(resp as unknown as T)
  }

  const deviceRenameMatch = /^\/api\/v1\/devices\/([^/]+)$/.exec(path)
  if (deviceRenameMatch && method === 'PATCH') {
    const id = deviceRenameMatch[1]
    requireDevice(id)
    const name = String((opts.body as { name?: string } | undefined)?.name ?? '').trim()
    if (!name) throw new MockApiError(400, 'name must be 1-100 characters')
    updateDevice(id, (d) => ({ ...d, name }))
    return delay(requireDevice(id) as unknown as T)
  }

  const deviceRotateMatch = /^\/api\/v1\/devices\/([^/]+)\/rotate$/.exec(path)
  if (deviceRotateMatch && method === 'POST') {
    const id = deviceRotateMatch[1]
    const existing = requireDevice(id)
    const tokenCreatedAt = new Date().toISOString()
    // Un-revokes the device (rotating a revoked device's token brings it back
    // to life, per the real server contract) and bumps token_created_at.
    updateDevice(id, (d) => ({ ...d, token_created_at: tokenCreatedAt, revoked_at: undefined }))
    const resp: RotateDeviceResponse = {
      id,
      name: existing.name,
      token: `aum_${hashStr(id + tokenCreatedAt).toString(16)}${Date.now().toString(16)}`,
      token_created_at: tokenCreatedAt,
    }
    return delay(resp as unknown as T)
  }

  const deviceResyncMatch = /^\/api\/v1\/devices\/([^/]+)\/resync$/.exec(path)
  if (deviceResyncMatch && method === 'POST') {
    const id = deviceResyncMatch[1]
    requireDevice(id)
    const resp: ResyncDeviceResponse = { id, resync_requested: true }
    return delay(resp as unknown as T)
  }

  const deviceEventsMatch = /^\/api\/v1\/devices\/([^/]+)\/events$/.exec(path)
  if (deviceEventsMatch && method === 'DELETE') {
    const id = deviceEventsMatch[1]
    requireDevice(id)
    const deleted = purgeDeviceRecords(id)
    updateDevice(id, (d) => ({ ...d, events_total: 0 }))
    const resp: PurgeDeviceEventsResponse = { deleted_events: deleted, resync_requested: true }
    return delay(resp as unknown as T)
  }

  const deviceDeleteMatch = /^\/api\/v1\/devices\/([^/]+)$/.exec(path)
  if (deviceDeleteMatch && method === 'DELETE') {
    const id = deviceDeleteMatch[1]
    requireDevice(id)
    if (params.purge === 'true') {
      const deleted = purgeDeviceRecords(id)
      setDevices(listDevices().filter((d) => d.id !== id))
      const resp: PurgeDeviceResponse = { deleted_events: deleted, purged: true }
      return delay(resp as unknown as T)
    }
    // Real server behavior: revoke the token but keep the row (events stay
    // intact) — never remove the device from the list.
    const revokedAt = new Date().toISOString()
    updateDevice(id, (d) => ({ ...d, revoked_at: d.revoked_at ?? revokedAt }))
    const resp: DeleteDeviceResponse = { id, revoked: true }
    return delay(resp as unknown as T)
  }

  if (path === '/api/v1/summary') {
    if (MOCK_FAIL === 'summary') throw new MockApiError(500, 'mock: forced failure via VITE_MOCK_FAIL=summary')
    return delay(buildSummary(params) as unknown as T)
  }
  if (path === '/api/v1/timeseries') return delay(buildTimeseries(params) as unknown as T)
  if (path === '/api/v1/breakdown') return delay(buildBreakdown(params) as unknown as T)
  if (path === '/api/v1/events') return delay(buildEvents(params) as unknown as T)
  if (path === '/api/v1/throughput') return delay(buildThroughput(params) as unknown as T)
  if (path === '/api/v1/pricing' && method === 'GET') return delay(buildPricing() as unknown as T)
  if (path === '/api/v1/pricing/reload' && method === 'POST') return delay(buildPricing() as unknown as T)
  if (path === '/downloads/manifest.json') return delay(buildManifest() as unknown as T)
  if (path === '/health') return delay(buildHealth() as unknown as T)

  throw new MockApiError(404, `mock: no handler for ${method} ${path}`)
}
