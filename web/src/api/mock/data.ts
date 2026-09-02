// The mock backend's dataset: deterministic reference data plus the synthetic
// day-level usage records every endpoint is derived from. This module owns all
// mutable mock state (devices, records) and exposes it through accessors so
// handlers.ts can mutate it without cross-module binding tricks.
//
// Nothing here knows about HTTP or docs/API.md response shapes — that is
// handlers.ts's job.
import type { Device, Source, UsageEventDTO } from '../types'

// ---- deterministic PRNG -----------------------------------------------

export function hashStr(s: string): number {
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

/** A PRNG seeded by the given parts — same parts, same stream, every reload. */
export function rngFor(...parts: string[]) {
  return mulberry32(hashStr(parts.join('|')))
}

function kstDateString(d: Date): string {
  return new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Seoul' }).format(d)
}

export function addDays(dateStr: string, days: number): string {
  const d = new Date(`${dateStr}T00:00:00Z`)
  d.setUTCDate(d.getUTCDate() + days)
  return d.toISOString().slice(0, 10)
}

export const TODAY = kstDateString(new Date())

// ---- static reference data --------------------------------------------

export interface MockModel {
  model: string
  priceUsdPer1M: { input: number; output: number; cache_read: number; cache_write: number } | null
  estimated: boolean
}

// Deliberately >15 distinct models across sources (was 10) so the Models
// page's "top 15 + 전체 보기" cutoff has something real to expand, and sizes
// vary widely (unpriced small models next to expensive frontier ones) to
// exercise both the cost and token-consumption charts.
export const MODELS: Record<Source, MockModel[]> = {
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

/** Current device list. Handlers read through this so they always see the
 * effect of an earlier add/rotate/revoke in the same session. */
export function listDevices(): Device[] {
  return devices
}

export function setDevices(next: Device[]): void {
  devices = next
}

// ---- synthetic dataset ---------------------------------------------------

export interface DayRecord {
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

/** The day-level records currently in the mock store. */
export function dataset(): DayRecord[] {
  return DATASET
}

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

export function flatEvents(): UsageEventDTO[] {
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
        session_id: sessionKey(rec),
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

/** The session id a record's events are reported under, so /breakdown and
 * /timeseries group by exactly the sessions /events hands out. */
export function sessionKey(r: DayRecord): string {
  return `sess-${hashStr(r.date + r.deviceId).toString(16)}`
}

/** Removes every record for a device (재동기화/완전 삭제) so
 * summary/timeseries/breakdown/events immediately reflect the purge, and
 * returns the number of events removed (mirrors the server's `deleted_events`). */
export function purgeDeviceRecords(deviceId: string): number {
  const deleted = DATASET.filter((r) => r.deviceId === deviceId).reduce((s, r) => s + r.events, 0)
  DATASET = DATASET.filter((r) => r.deviceId !== deviceId)
  flatEventsCache = null
  return deleted
}
