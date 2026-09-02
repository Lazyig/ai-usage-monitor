// Deterministic mock backend used when VITE_MOCK=1. Mirrors docs/API.md
// shapes exactly so every page can be visually sanity-checked without a
// running Go server. Not included in the default production code path
// beyond this guarded import (see client.ts).
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
  RotateDeviceResponse,
  Source,
  SummaryResponse,
  SummaryRowBase,
  TimeseriesResponse,
  UsageEventDTO,
  UsageTotals,
} from './types'

// Debug-only toggle (see web/README.md verification notes): set
// VITE_MOCK_FAIL=summary to make GET /api/v1/summary fail with a 500,
// so the per-widget error rendering on the Overview page can be eyeballed
// without a real backend.
const MOCK_FAIL = import.meta.env.VITE_MOCK_FAIL as string | undefined

// ---- deterministic PRNG -----------------------------------------------

function hashStr(s: string): number {
  let h = 2166136261
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i)
    h = Math.imul(h, 16777619)
  }
  return h >>> 0
}

function mulberry32(seed: number) {
  let a = seed
  return () => {
    a |= 0
    a = (a + 0x6d2b79f5) | 0
    let t = Math.imul(a ^ (a >>> 15), 1 | a)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

function rngFor(...parts: string[]) {
  return mulberry32(hashStr(parts.join('|')))
}

function kstDateString(d: Date): string {
  return new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Seoul' }).format(d)
}

function addDays(dateStr: string, days: number): string {
  const d = new Date(`${dateStr}T00:00:00Z`)
  d.setUTCDate(d.getUTCDate() + days)
  return d.toISOString().slice(0, 10)
}

const TODAY = kstDateString(new Date())

// ---- static reference data --------------------------------------------

interface MockModel {
  model: string
  priceUsdPer1M: { input: number; output: number; cache_read: number; cache_write: number } | null
  estimated: boolean
}

// Deliberately >15 distinct models across sources (was 10) so the Models
// page's "top 15 + 전체 보기" cutoff has something real to expand, and sizes
// vary widely (unpriced small models next to expensive frontier ones) to
// exercise both the cost and token-consumption charts.
const MODELS: Record<Source, MockModel[]> = {
  claude_code: [
    { model: 'claude-opus-5', priceUsdPer1M: { input: 5, output: 25, cache_read: 0.5, cache_write: 6.25 }, estimated: false },
    { model: 'claude-sonnet-5', priceUsdPer1M: { input: 2, output: 10, cache_read: 0.2, cache_write: 2.5 }, estimated: false },
    { model: 'claude-fable-5', priceUsdPer1M: { input: 10, output: 50, cache_read: 1, cache_write: 12.5 }, estimated: false },
    { model: 'claude-haiku-5', priceUsdPer1M: { input: 0.8, output: 4, cache_read: 0.08, cache_write: 1 }, estimated: false },
    { model: 'claude-opus-5-1', priceUsdPer1M: { input: 6, output: 30, cache_read: 0.6, cache_write: 7.5 }, estimated: false },
  ],
  codex: [
    { model: 'gpt-5.6-terra', priceUsdPer1M: null, estimated: false },
    { model: 'gpt-5.6-sol', priceUsdPer1M: null, estimated: false },
    { model: 'codex-auto-review', priceUsdPer1M: null, estimated: false },
    { model: 'gpt-5.6-nano', priceUsdPer1M: null, estimated: false },
    { model: 'gpt-5.7-preview', priceUsdPer1M: null, estimated: false },
  ],
  antigravity: [
    { model: 'gemini-3.7-flash', priceUsdPer1M: { input: 0.3, output: 1.2, cache_read: 0.03, cache_write: 0.375 }, estimated: false },
    { model: 'gemini-default', priceUsdPer1M: { input: 0.3, output: 1.2, cache_read: 0.03, cache_write: 0.375 }, estimated: true },
    { model: 'claude-sonnet-4-6', priceUsdPer1M: { input: 3, output: 15, cache_read: 0.3, cache_write: 3.75 }, estimated: false },
    { model: 'gemini-3-flash-a', priceUsdPer1M: null, estimated: false },
    { model: 'gemini-3-pro', priceUsdPer1M: { input: 2, output: 8, cache_read: 0.2, cache_write: 2.5 }, estimated: false },
    { model: 'gemini-2.5-flash-lite', priceUsdPer1M: { input: 0.15, output: 0.6, cache_read: 0.015, cache_write: 0.19 }, estimated: false },
  ],
}

const PROJECTS = [
  '/home/kim/work/ai-usage-monitor',
  '/home/kim/work/discord-bot-fleet',
  '/home/kim/projects/palworld-pal-info-service',
  'file:///Users/kim/Documents/notes-vault',
  '/home/kim/work/ark-asa-server-tools',
]

interface MockDeviceDef {
  id: string
  name: string
  os: string
  arch: string
  agent_version: string
  sources: Source[]
  staleMinutes: number
  tokenAgeDays: number
}

const DEVICE_DEFS: MockDeviceDef[] = [
  { id: '11111111-1111-4111-8111-111111111111', name: 'macbook-pro', os: 'darwin', arch: 'arm64', agent_version: '0.3.1', sources: ['claude_code', 'codex'], staleMinutes: 2, tokenAgeDays: 40 },
  { id: '22222222-2222-4222-8222-222222222222', name: 'desktop-win', os: 'windows', arch: 'amd64', agent_version: '0.3.0', sources: ['codex', 'antigravity'], staleMinutes: 27, tokenAgeDays: 21 },
  { id: '33333333-3333-4333-8333-333333333333', name: 'thinkpad', os: 'linux', arch: 'amd64', agent_version: '0.3.1', sources: ['claude_code', 'antigravity'], staleMinutes: 4, tokenAgeDays: 65 },
]

// devices are mutable in-session so Add/Revoke on the Devices page works
let devices: Device[] = DEVICE_DEFS.map((d) => ({
  id: d.id,
  name: d.name,
  os: d.os,
  arch: d.arch,
  agent_version: d.agent_version,
  last_seen: new Date(Date.now() - d.staleMinutes * 60_000).toISOString(),
  last_ingest: new Date(Date.now() - d.staleMinutes * 60_000).toISOString(),
  events_total: 0, // filled in after dataset generation
  token_created_at: new Date(Date.now() - d.tokenAgeDays * 86_400_000).toISOString(),
}))

// One already-revoked device, exercising the "폐기됨" UI states — never part
// of DEVICE_DEFS/DATASET since its token no longer ingests anything.
devices.push({
  id: '44444444-4444-4444-8444-444444444444',
  name: 'old-laptop (폐기됨)',
  os: 'linux',
  arch: 'amd64',
  agent_version: '0.2.0',
  last_seen: new Date(Date.now() - 12 * 86_400_000).toISOString(),
  last_ingest: new Date(Date.now() - 12 * 86_400_000).toISOString(),
  events_total: 412,
  token_created_at: new Date(Date.now() - 90 * 86_400_000).toISOString(),
  revoked_at: new Date(Date.now() - 5 * 86_400_000).toISOString(),
})

// ---- synthetic dataset ---------------------------------------------------

interface DayRecord {
  date: string // YYYY-MM-DD (KST)
  source: Source
  deviceId: string
  deviceName: string
  model: string
  estimated: boolean
  project: string
  events: number
  input: number
  output: number
  cache_read: number
  cache_write: number
  reasoning: number
  cost_usd: number | null
}

const DATASET_DAYS = 45

function costFor(model: MockModel, input: number, output: number, cacheRead: number, cacheWrite: number): number | null {
  if (!model.priceUsdPer1M) return null
  const p = model.priceUsdPer1M
  return (input * p.input + output * p.output + cacheRead * p.cache_read + cacheWrite * p.cache_write) / 1_000_000
}

function buildDataset(): DayRecord[] {
  const records: DayRecord[] = []
  for (let offset = 0; offset < DATASET_DAYS; offset++) {
    const date = addDays(TODAY, -offset)
    for (const dev of DEVICE_DEFS) {
      for (const source of dev.sources) {
        const activityRng = rngFor('active', date, dev.id, source)
        if (activityRng() < 0.12) continue // simulate an idle day

        const models = MODELS[source]
        const modelCount = 1 + Math.floor(rngFor('modelcount', date, dev.id, source)() * 2)
        for (let mi = 0; mi < modelCount && mi < models.length; mi++) {
          const model = models[(hashStr(date + dev.id + source) + mi) % models.length]
          const r = rngFor('rec', date, dev.id, source, model.model)
          const events = 3 + Math.floor(r() * 35)
          const input = Math.round(events * (150 + r() * 900))
          const output = Math.round(events * (80 + r() * 450))
          const cache_read = Math.round(input * (0.2 + r() * 0.5))
          const cache_write = Math.round(input * (0.03 + r() * 0.12))
          const reasoning = source === 'codex' ? Math.round(output * (0.1 + r() * 0.25)) : 0
          const project = PROJECTS[Math.floor(r() * PROJECTS.length)]
          records.push({
            date,
            source,
            deviceId: dev.id,
            deviceName: dev.name,
            model: model.model,
            estimated: model.estimated,
            project,
            events,
            input,
            output,
            cache_read,
            cache_write,
            reasoning,
            cost_usd: costFor(model, input, output, cache_read, cache_write),
          })
        }
      }
    }
  }
  return records
}

// mutable: purge (재동기화/완전 삭제) actions remove a device's records so the
// other mock endpoints (summary/timeseries/breakdown/events) reflect it too
let DATASET = buildDataset()

// fill device event totals now that the dataset exists (only for devices
// that are actually part of the synthetic dataset — leaves e.g. the
// already-revoked device's manually-set total alone)
const DATASET_DEVICE_IDS = new Set(DEVICE_DEFS.map((d) => d.id))
devices = devices.map((d) =>
  DATASET_DEVICE_IDS.has(d.id)
    ? { ...d, events_total: DATASET.filter((r) => r.deviceId === d.id).reduce((s, r) => s + r.events, 0) }
    : d,
)

// ---- flat event-level rows (for /events), generated lazily -----------

let flatEventsCache: UsageEventDTO[] | null = null

function flatEvents(): UsageEventDTO[] {
  if (flatEventsCache) return flatEventsCache
  const out: UsageEventDTO[] = []
  for (const rec of DATASET) {
    const n = Math.max(1, Math.min(rec.events, 40))
    for (let i = 0; i < n; i++) {
      const r = rngFor('evt', rec.date, rec.deviceId, rec.model, String(i))
      const frac = 1 / n
      const hh = Math.floor(r() * 24)
      const mm = Math.floor(r() * 60)
      const ss = Math.floor(r() * 60)
      const ts = `${rec.date}T${String(hh).padStart(2, '0')}:${String(mm).padStart(2, '0')}:${String(ss).padStart(2, '0')}Z`
      const input = Math.round(rec.input * frac)
      const output = Math.round(rec.output * frac)
      const cache_read = Math.round(rec.cache_read * frac)
      const cache_write = Math.round(rec.cache_write * frac)
      const reasoning = Math.round(rec.reasoning * frac)
      out.push({
        event_id: hashStr(rec.date + rec.deviceId + rec.model + i).toString(16),
        device_id: rec.deviceId,
        source: rec.source,
        ts,
        model: rec.model,
        session_id: `sess-${hashStr(rec.date + rec.deviceId).toString(16)}`,
        project: rec.project,
        input,
        output,
        cache_read,
        cache_write_5m: rec.source === 'claude_code' ? cache_write : 0,
        cache_write_1h: 0,
        cache_write: rec.source === 'claude_code' ? 0 : cache_write,
        reasoning,
        confidence: 'exact',
        tool_version: '0.3.1',
        cost_usd: rec.cost_usd === null ? null : rec.cost_usd * frac,
      })
    }
  }
  out.sort((a, b) => (a.ts < b.ts ? 1 : -1))
  flatEventsCache = out
  return out
}

// Removes every DATASET/flatEvents record for a device (재동기화/완전 삭제) so
// summary/timeseries/breakdown/events immediately reflect the purge, and
// returns the number of events removed (mirrors the server's `deleted_events`).
function purgeDeviceRecords(deviceId: string): number {
  const deleted = DATASET.filter((r) => r.deviceId === deviceId).reduce((s, r) => s + r.events, 0)
  DATASET = DATASET.filter((r) => r.deviceId !== deviceId)
  flatEventsCache = null
  return deleted
}

// ---- request helpers ---------------------------------------------------

function inRange(date: string, from: string, to: string): boolean {
  return date >= from && date < to
}

function normalizeRange(params: Record<string, string | undefined>): { from: string; to: string } {
  const to = params.to ? params.to.slice(0, 10) : addDays(TODAY, 1)
  const from = params.from ? params.from.slice(0, 10) : addDays(to, -30)
  return { from, to }
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

function buildSummary(params: Record<string, string | undefined>): SummaryResponse {
  const { from, to } = normalizeRange(params)
  const rows = DATASET.filter((r) => inRange(r.date, from, to))

  const totals: UsageTotals = { input: 0, output: 0, cache_read: 0, cache_write: 0, reasoning: 0, cost_usd: 0, cost_known_ratio: 0, events: 0 }
  let knownTokens = 0
  let totalTokens = 0
  let anyKnownCost = false
  const bySource = new Map<string, SummaryRowBase>()
  const byDevice = new Map<string, SummaryRowBase & { name: string }>()
  const byModel = new Map<string, SummaryRowBase & { source: Source; estimated: boolean }>()

  for (const r of rows) {
    mergeRow(totals, r)
    const tokens = r.input + r.output + r.cache_read + r.cache_write
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

  return {
    from: `${from}T00:00:00Z`,
    to: `${to}T00:00:00Z`,
    totals,
    by_source: [...bySource.values()].sort((a, b) => (b.cost_usd ?? 0) - (a.cost_usd ?? 0)),
    by_device: [...byDevice.values()].sort((a, b) => (b.cost_usd ?? 0) - (a.cost_usd ?? 0)),
    by_model: [...byModel.values()].sort((a, b) => (b.cost_usd ?? 0) - (a.cost_usd ?? 0)),
  }
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

function buildTimeseries(params: Record<string, string | undefined>): TimeseriesResponse {
  const { from, to } = normalizeRange(params)
  const bucket = params.bucket === 'hour' ? 'hour' : 'day'
  const groupBy = (params.group_by as GroupBy) ?? 'source'
  let rows = DATASET.filter((r) => inRange(r.date, from, to))
  if (params.source) rows = rows.filter((r) => r.source === params.source)

  const keyOf = (r: DayRecord): string => {
    switch (groupBy) {
      case 'device':
        return r.deviceName
      case 'model':
        return r.model
      case 'project':
        return r.project
      default:
        return r.source
    }
  }

  const series = new Map<
    string,
    Map<string, { input: number; output: number; cache_read: number; cache_write: number; reasoning: number; cost_usd: number | null; events: number }>
  >()

  for (const r of rows) {
    const key = keyOf(r)
    if (!series.has(key)) series.set(key, new Map())
    const points = series.get(key)!

    if (bucket === 'day') {
      const p = points.get(r.date) ?? { input: 0, output: 0, cache_read: 0, cache_write: 0, reasoning: 0, cost_usd: null, events: 0 }
      p.input += r.input
      p.output += r.output
      p.cache_read += r.cache_read
      p.cache_write += r.cache_write
      p.reasoning += r.reasoning
      p.events += r.events
      if (r.cost_usd !== null) p.cost_usd = (p.cost_usd ?? 0) + r.cost_usd
      points.set(r.date, p)
    } else {
      // spread the day's total across a few representative hours
      const hrRng = rngFor('hr', r.date, r.deviceId, r.model)
      const hours = [9, 11, 14, 16, 20].filter(() => hrRng() > 0.3)
      const bucketHours = hours.length > 0 ? hours : [12]
      for (const h of bucketHours) {
        const t = `${r.date}T${String(h).padStart(2, '0')}:00:00Z`
        const frac = 1 / bucketHours.length
        const p = points.get(t) ?? { input: 0, output: 0, cache_read: 0, cache_write: 0, reasoning: 0, cost_usd: null, events: 0 }
        p.input += Math.round(r.input * frac)
        p.output += Math.round(r.output * frac)
        p.cache_read += Math.round(r.cache_read * frac)
        p.cache_write += Math.round(r.cache_write * frac)
        p.reasoning += Math.round(r.reasoning * frac)
        p.events += Math.round(r.events * frac)
        if (r.cost_usd !== null) p.cost_usd = (p.cost_usd ?? 0) + r.cost_usd * frac
        points.set(t, p)
      }
    }
  }

  return {
    bucket,
    group_by: groupBy,
    series: [...series.entries()]
      .map(([key, points]) => ({
        key,
        points: [...points.entries()]
          .sort(([a], [b]) => (a < b ? -1 : 1))
          .map(([t, v]) => ({ t, ...v })),
      }))
      .sort((a, b) => a.key.localeCompare(b.key)),
  }
}

function buildBreakdown(params: Record<string, string | undefined>): BreakdownResponse {
  const { from, to } = normalizeRange(params)
  const groupBy = (params.group_by as BreakdownResponse['group_by']) ?? 'model'
  const limit = params.limit ? parseInt(params.limit, 10) : 50
  let rows = DATASET.filter((r) => inRange(r.date, from, to))
  if (params.source) rows = rows.filter((r) => r.source === params.source)
  if (params.device) rows = rows.filter((r) => r.deviceId === params.device)

  const keyOf = (r: DayRecord): string => {
    switch (groupBy) {
      case 'project':
        return r.project
      case 'device':
        return r.deviceName
      case 'session':
        return `sess-${hashStr(r.date + r.deviceId).toString(16)}`
      default:
        return r.model
    }
  }

  const acc = new Map<string, BreakdownRow>()
  for (const r of rows) {
    const key = keyOf(r)
    const existing = acc.get(key)
    if (!existing) {
      acc.set(key, {
        key,
        source: r.source,
        device: r.deviceName,
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
    }
  }

  const rowsOut = [...acc.values()].sort((a, b) => (b.cost_usd ?? 0) - (a.cost_usd ?? 0)).slice(0, limit)
  return { group_by: groupBy, rows: rowsOut }
}

function buildEvents(params: Record<string, string | undefined>): EventsResponse {
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
        effective_from: p ? '2026-06-01T00:00:00Z' : undefined,
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
  params?: Record<string, string | undefined>
  body?: unknown
  token?: string | null
}

const DELAY_MS = 180

function delay<T>(value: T): Promise<T> {
  return new Promise((resolve) => setTimeout(() => resolve(value), DELAY_MS))
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
    return delay({ devices } as unknown as T)
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
    devices = [...devices, newDevice]
    const resp: CreateDeviceResponse = { id, name: body.name, token: `aum_${hashStr(id).toString(16)}${Date.now().toString(16)}` }
    return delay(resp as unknown as T)
  }

  const deviceRotateMatch = /^\/api\/v1\/devices\/([^/]+)\/rotate$/.exec(path)
  if (deviceRotateMatch && method === 'POST') {
    const id = deviceRotateMatch[1]
    const existing = devices.find((d) => d.id === id)
    if (!existing) throw new MockApiError(404, 'device not found')
    const tokenCreatedAt = new Date().toISOString()
    // Un-revokes the device (rotating a revoked device's token brings it back
    // to life, per the real server contract) and bumps token_created_at.
    devices = devices.map((d) => (d.id === id ? { ...d, token_created_at: tokenCreatedAt, revoked_at: undefined } : d))
    const resp: RotateDeviceResponse = {
      id,
      name: existing.name,
      token: `aum_${hashStr(id + tokenCreatedAt).toString(16)}${Date.now().toString(16)}`,
      token_created_at: tokenCreatedAt,
    }
    return delay(resp as unknown as T)
  }

  const deviceEventsMatch = /^\/api\/v1\/devices\/([^/]+)\/events$/.exec(path)
  if (deviceEventsMatch && method === 'DELETE') {
    const id = deviceEventsMatch[1]
    const existing = devices.find((d) => d.id === id)
    if (!existing) throw new MockApiError(404, 'device not found')
    const deleted = purgeDeviceRecords(id)
    devices = devices.map((d) => (d.id === id ? { ...d, events_total: 0 } : d))
    const resp: PurgeDeviceEventsResponse = { deleted_events: deleted, resync_requested: true }
    return delay(resp as unknown as T)
  }

  const deviceDeleteMatch = /^\/api\/v1\/devices\/([^/]+)$/.exec(path)
  if (deviceDeleteMatch && method === 'DELETE') {
    const id = deviceDeleteMatch[1]
    const existing = devices.find((d) => d.id === id)
    if (!existing) throw new MockApiError(404, 'device not found')
    if (params.purge === 'true') {
      const deleted = purgeDeviceRecords(id)
      devices = devices.filter((d) => d.id !== id)
      const resp: PurgeDeviceResponse = { deleted_events: deleted, purged: true }
      return delay(resp as unknown as T)
    }
    // Real server behavior: revoke the token but keep the row (events stay
    // intact) — never remove the device from the list.
    const revokedAt = new Date().toISOString()
    devices = devices.map((d) => (d.id === id ? { ...d, revoked_at: d.revoked_at ?? revokedAt } : d))
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
  if (path === '/api/v1/pricing' && method === 'GET') return delay(buildPricing() as unknown as T)
  if (path === '/api/v1/pricing/reload' && method === 'POST') return delay(buildPricing() as unknown as T)
  if (path === '/downloads/manifest.json') return delay(buildManifest() as unknown as T)
  if (path === '/health') return delay(buildHealth() as unknown as T)

  throw new MockApiError(404, `mock: no handler for ${method} ${path}`)
}
