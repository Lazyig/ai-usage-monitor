// Single fetch wrapper for the admin API. Adds the Bearer token, surfaces
// 401s to the auth layer, and (when VITE_MOCK=1) transparently serves
// deterministic fake data shaped exactly like docs/API.md instead of
// touching the network — see mock/handlers.ts.
import { mockRequest } from './mock/handlers'
import type {
  BreakdownGroupBy,
  BreakdownResponse,
  CreateDeviceResponse,
  DeleteDeviceResponse,
  Device,
  DevicesResponse,
  DownloadsManifest,
  EventsResponse,
  GroupBy,
  HealthResponse,
  PricingResponse,
  PurgeDeviceEventsResponse,
  PurgeDeviceResponse,
  RotateDeviceResponse,
  SummaryResponse,
  ThroughputResponse,
  TimeseriesBucket,
  TimeseriesResponse,
} from './types'
import { readStored, removeStored, writeStored } from '../lib/storage'

const MOCK = import.meta.env.VITE_MOCK === '1'
const TOKEN_KEY = 'aum_admin_token'

let authToken: string | null = readStored(TOKEN_KEY)
let unauthorizedHandler: (() => void) | null = null

export function getStoredToken(): string | null {
  return authToken
}

export function setStoredToken(token: string): void {
  authToken = token
  writeStored(TOKEN_KEY, token)
}

export function clearStoredToken(): void {
  authToken = null
  removeStored(TOKEN_KEY)
}

/** Registered once by AuthContext; called whenever any request gets a 401. */
export function onUnauthorized(handler: () => void): void {
  unauthorizedHandler = handler
}

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
    this.name = 'ApiError'
  }
}

type Params = Record<string, string | undefined>

/** Endpoint functions declare their params with real types (numbers stay
 * numbers); this stringifies them on the way out so no call site has to. */
function toParams(params: object): Params {
  const out: Params = {}
  for (const [k, v] of Object.entries(params)) out[k] = v === undefined ? undefined : String(v)
  return out
}

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PATCH' | 'DELETE'
  params?: Params
  body?: unknown
  /** Override the stored token, used only by the login screen to verify a token before storing it. */
  token?: string
}

async function safeJson(res: Response): Promise<{ error?: string } | null> {
  try {
    return await res.json()
  } catch {
    return null
  }
}

async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const token = opts.token ?? authToken

  if (MOCK) {
    try {
      return await mockRequest<T>(path, { method: opts.method, params: opts.params, body: opts.body, token })
    } catch (e) {
      const err = e as { status?: number; message: string }
      if (err.status === 401) unauthorizedHandler?.()
      throw new ApiError(err.status ?? 500, err.message)
    }
  }

  const url = new URL(path, window.location.origin)
  if (opts.params) {
    for (const [k, v] of Object.entries(opts.params)) {
      if (v !== undefined && v !== '') url.searchParams.set(k, v)
    }
  }

  const res = await fetch(url.pathname + url.search, {
    method: opts.method ?? 'GET',
    headers: {
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(opts.body ? { 'Content-Type': 'application/json' } : {}),
    },
    body: opts.body ? JSON.stringify(opts.body) : undefined,
  })

  if (res.status === 401) {
    unauthorizedHandler?.()
    const data = await safeJson(res)
    throw new ApiError(401, data?.error ?? 'unauthorized')
  }
  if (!res.ok) {
    const data = await safeJson(res)
    throw new ApiError(res.status, data?.error ?? `요청 실패 (${res.status})`)
  }
  if (res.status === 204) return undefined as T
  const text = await res.text()
  if (!text) return undefined as T
  return JSON.parse(text) as T
}

// ---- auth ---------------------------------------------------------------

/** Verifies a token without touching the stored one; used by the login screen. */
export function verifyToken(token: string): Promise<DevicesResponse> {
  return request<DevicesResponse>('/api/v1/devices', { token })
}

// ---- read endpoints -------------------------------------------------------

/** The `from`/`to`/`tz` window every aggregate endpoint accepts. `to` is
 * exclusive; both default server-side to "the last 30 days". */
export interface RangeParams {
  from?: string
  to?: string
  tz?: string
}

/** The narrowing filters shared by summary/timeseries/breakdown/events. */
export interface FilterParams {
  source?: string
  device?: string
}

export function getSummary(params: RangeParams & FilterParams): Promise<SummaryResponse> {
  return request('/api/v1/summary', { params: toParams(params) })
}

export function getTimeseries(
  params: RangeParams & FilterParams & { bucket?: TimeseriesBucket; group_by?: GroupBy },
): Promise<TimeseriesResponse> {
  return request('/api/v1/timeseries', { params: toParams(params) })
}

export function getBreakdown(
  params: RangeParams & FilterParams & { group_by: BreakdownGroupBy; limit?: number },
): Promise<BreakdownResponse> {
  return request('/api/v1/breakdown', { params: toParams(params) })
}

export function getThroughput(params: RangeParams & FilterParams & { group_by?: GroupBy }): Promise<ThroughputResponse> {
  return request('/api/v1/throughput', { params: toParams(params) })
}

export function getEvents(
  params: RangeParams & FilterParams & { model?: string; limit?: number; cursor?: string },
): Promise<EventsResponse> {
  return request('/api/v1/events', { params: toParams(params) })
}

export function getDevices(): Promise<DevicesResponse> {
  return request('/api/v1/devices')
}

export function createDevice(name: string): Promise<CreateDeviceResponse> {
  return request('/api/v1/devices', { method: 'POST', body: { name } })
}

/** Revokes a device's token. The server replies `200` with a small JSON body
 * (`{"id","revoked"}`) or, tolerated here just in case, `204` with none. */
export function deleteDevice(id: string): Promise<DeleteDeviceResponse | undefined> {
  return request(`/api/v1/devices/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

/** Issues a fresh token for the device; the old one stops working immediately.
 * Un-revokes a previously-revoked device. Device id and event history are kept. */
/** Sets the operator-owned display name (agents can no longer overwrite it). */
export function renameDevice(id: string, name: string): Promise<Device> {
  return request(`/api/v1/devices/${encodeURIComponent(id)}`, { method: 'PATCH', body: { name } })
}

export function rotateDevice(id: string): Promise<RotateDeviceResponse> {
  return request(`/api/v1/devices/${encodeURIComponent(id)}/rotate`, { method: 'POST' })
}

/** Deletes the device's events only; the agent re-sends its full history on
 * its next heartbeat (<= 1 min). Used for "재동기화". */
export function purgeDeviceEvents(id: string): Promise<PurgeDeviceEventsResponse> {
  return request(`/api/v1/devices/${encodeURIComponent(id)}/events`, { method: 'DELETE' })
}

/** Deletes the device's events AND the device itself. Irreversible. */
export function purgeDevice(id: string): Promise<PurgeDeviceResponse> {
  return request(`/api/v1/devices/${encodeURIComponent(id)}`, { method: 'DELETE', params: { purge: 'true' } })
}

export function getPricing(): Promise<PricingResponse> {
  return request('/api/v1/pricing')
}

export function reloadPricing(): Promise<PricingResponse> {
  return request('/api/v1/pricing/reload', { method: 'POST' })
}

export function getDownloadsManifest(): Promise<DownloadsManifest> {
  return request('/downloads/manifest.json')
}

export function getHealth(): Promise<HealthResponse> {
  return request('/health')
}
