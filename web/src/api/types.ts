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
  /** Share of the row's tokens that had a price (1 when there are none). */
  cost_known_ratio?: number
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
export type GroupBy = 'source' | 'device' | 'model' | 'project' | 'session'

export interface TimeseriesPoint {
  t: string
  input: number
  output: number
  cache_read: number
  cache_write: number
  reasoning: number
  cost_usd: number | null
  events: number
}

export interface TimeseriesSeries {
  /** For group_by=device this is the device UUID (see `name`). */
  key: string
  /** Human-readable device name; only populated for group_by=device. */
  name?: string
  points: TimeseriesPoint[]
}

export interface TimeseriesResponse {
  bucket: TimeseriesBucket
  group_by: GroupBy
  series: TimeseriesSeries[]
  /** IANA zone the buckets were computed in; omitted by older servers. */
  tz?: string
}

export type BreakdownGroupBy = 'model' | 'project' | 'device' | 'session' | 'source'

export interface BreakdownRow {
  /** For group_by=device this is the device UUID (see `name`). */
  key: string
  /** Human-readable device name; only populated for group_by=device. */
  name?: string
  /** Only set when every event behind the row shares one source. */
  source?: Source
  /** Only set when every event behind the row shares one device. */
  device?: string
  input: number
  output: number
  cache_read: number
  cache_write: number
  reasoning: number
  cost_usd: number | null
  /** Share of tokens whose cost is known; omitted by older servers. */
  cost_known_ratio?: number
  estimated: boolean
  events: number
  first_seen: string
  last_seen: string
}

export interface BreakdownResponse {
  group_by: BreakdownGroupBy
  rows: BreakdownRow[]
}

/** The busiest minute of one token class (GET /throughput). */
export interface ThroughputPeak {
  /** Minute start, RFC3339 in the response `tz`. */
  t: string
  tokens: number
}

/** One row of GET /throughput: tokens per *active* minute. A minute is active
 * when at least one event completed in it, so idle time never dilutes the
 * rate. `prompt` = input + cache_read + cache_write. */
export interface ThroughputGroup {
  key: string
  /** Human-readable device name; only populated for group_by=device. */
  name?: string
  active_minutes: number
  prompt: number
  input: number
  cache_read: number
  cache_write: number
  output: number
  events: number
  per_minute: { prompt: number; output: number; total: number }
  /** Omitted when the range holds no events. */
  peak_prompt?: ThroughputPeak
  peak_output?: ThroughputPeak
}

export interface ThroughputResponse {
  group_by: GroupBy
  tz: string
  /** Every group folded together; active minutes are the union. */
  total: ThroughputGroup
  /** Busiest first (per_minute.total desc). */
  groups: ThroughputGroup[]
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
  /** null until the agent first reaches the server. */
  last_seen: string | null
  last_ingest: string | null
  events_total: number
  token_created_at: string
  /** Only present once the device's token has been revoked. */
  revoked_at?: string
  /** Hostname the agent last reported; `name` is operator-owned. */
  hostname?: string
}

/** GET/PUT /api/v1/preferences — the dashboard's last selections, kept on the
 * server. Absent fields mean "the default". */
export interface DashboardPreferences {
  range?: {
    preset: string
    /** Only for "custom": YYYY-MM-DD, `to` exclusive. */
    from?: string
    to?: string
  }
  auto_refresh?: boolean
}

/** GET /api/v1/server: the build the server runs. */
export interface ServerBuild {
  /** Release tag, `0.0.0-<sha>` for a main build, `dev` for a local one. */
  version: string
  /** Full git revision, when the binary was built in a checkout. */
  commit?: string
  /** RFC 3339 commit time. */
  commit_time?: string
  /** Built from a tree with uncommitted changes. */
  modified?: boolean
  go_version: string
}

export interface DevicesResponse {
  devices: Device[]
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

/** POST /api/v1/devices/{id}/rotate — new token shown once; the old token
 * stops working immediately; device id and history are kept. */
export interface RotateDeviceResponse {
  id: string
  name: string
  token: string
  token_created_at: string
}

/** DELETE /api/v1/devices/{id}/events — deletes the device's events only;
 * the agent re-sends its full history on its next heartbeat (<= 1 min). */
export interface PurgeDeviceEventsResponse {
  deleted_events: number
  resync_requested: boolean
}

/** POST /api/v1/devices/{id}/resync — asks the agent to re-read its logs from
 * zero without deleting anything; re-sent events dedupe, and copies that are
 * more complete than the stored row (responses shipped mid-stream) refresh it. */
export interface ResyncDeviceResponse {
  id: string
  resync_requested: boolean
}

/** DELETE /api/v1/devices/{id}?purge=true — deletes events AND the device. */
export interface PurgeDeviceResponse {
  deleted_events: number
  purged: boolean
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
  /** Exclusive end of the period this row applies to (YYYY-MM-DD). */
  effective_to?: string
  /** Free-text qualifier from pricing.yaml, e.g. "introductory pricing". */
  note?: string
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
