// Types mirroring docs/API.md. Keep field names identical to the JSON wire
// format (snake_case) since these are deserialized directly from the server.

export type Source = 'claude_code' | 'codex' | 'antigravity'

export type Confidence = 'exact' | 'estimated' | 'unknown'

/** Token + cost fields shared by every breakdown row shape in the API. */
export interface UsageTotals {
  input: number
  output: number
  cache_read: number
  cache_write: number
  reasoning: number
  cost_usd: number | null
  cost_known_ratio: number
  events: number
}

export interface SummaryResponse {
  from: string
  to: string
  totals: UsageTotals
  by_source: SummarySourceRow[]
  by_device: SummaryDeviceRow[]
  by_model: SummaryModelRow[]
}

/** Base aggregate fields present on every /summary breakdown row. */
export interface SummaryRowBase {
  key: string
  input: number
  output: number
  cache_read: number
  cache_write: number
  reasoning?: number
  cost_usd: number | null
  events: number
}

export type SummarySourceRow = SummaryRowBase

export interface SummaryDeviceRow extends SummaryRowBase {
  name: string
}

export interface SummaryModelRow extends SummaryRowBase {
  source: Source
  estimated: boolean
}

export type TimeseriesBucket = 'day' | 'hour'
export type GroupBy = 'source' | 'device' | 'model' | 'project'

export interface TimeseriesPoint {
  t: string
  input: number
  output: number
  cache_read: number
  cache_write: number
  cost_usd: number | null
}

export interface TimeseriesSeries {
  key: string
  points: TimeseriesPoint[]
}

export interface TimeseriesResponse {
  bucket: TimeseriesBucket
  group_by: GroupBy
  series: TimeseriesSeries[]
}

export type BreakdownGroupBy = 'model' | 'project' | 'device' | 'session'

export interface BreakdownRow {
  key: string
  source?: Source
  device?: string
  input: number
  output: number
  cache_read: number
  cache_write: number
  reasoning: number
  cost_usd: number | null
  estimated: boolean
  events: number
  first_seen: string
  last_seen: string
}

export interface BreakdownResponse {
  group_by: BreakdownGroupBy
  rows: BreakdownRow[]
}

/** UsageEvent as defined in internal/event, plus the server-computed cost. */
export interface UsageEventDTO {
  event_id: string
  device_id: string
  source: Source
  ts: string
  model: string
  session_id?: string
  project?: string
  input: number
  output: number
  cache_read: number
  cache_write_5m: number
  cache_write_1h: number
  cache_write: number
  reasoning: number
  confidence: Confidence
  tool_version?: string
  cost_usd: number | null
}

export interface EventsResponse {
  events: UsageEventDTO[]
  next_cursor?: string
}

export interface Device {
  id: string
  name: string
  os: string
  arch: string
  agent_version: string
  last_seen: string
  last_ingest: string
  events_total: number
  token_created_at: string
  /** Only present once the device's token has been revoked. */
  revoked_at?: string
}

export interface DevicesResponse {
  devices: Device[]
}

export interface CreateDeviceRequest {
  name: string
}

export interface CreateDeviceResponse {
  id: string
  name: string
  token: string
}

export interface DeleteDeviceResponse {
  id: string
  revoked: boolean
}

// Any numeric field can come back as `null` (price unknown) and provider /
// alias_of / effective_from can be missing entirely depending on the rate's
// shape in pricing.yaml — never assume they're present.
export interface PricingRate {
  match: string
  provider?: string
  input?: number | null
  output?: number | null
  cache_read?: number | null
  cache_write?: number | null
  cache_write_5m?: number | null
  cache_write_1h?: number | null
  alias_of?: string
  effective_from?: string
  estimated: boolean
}

export interface PricingResponse {
  rates: PricingRate[]
}

export interface DownloadFile {
  name: string
  os: string
  arch: string
  size: number
  sha256: string
}

export interface DownloadsManifest {
  version: string
  built_at: string
  files: DownloadFile[]
}

export interface HealthResponse {
  ok: boolean
  version: string
  events: number
}

export interface ApiErrorBody {
  error: string
}
