// Single fetch wrapper for the admin API. Adds the Bearer token, surfaces
// 401s to the auth layer, and (when VITE_MOCK=1) transparently serves
// deterministic fake data shaped exactly like docs/API.md instead of
// touching the network — see mock.ts.
import { mockRequest } from './mock'
import type {
  BreakdownGroupBy,
  BreakdownResponse,
  CreateDeviceResponse,
  DevicesResponse,
  DownloadsManifest,
  EventsResponse,
  GroupBy,
  HealthResponse,
  PricingResponse,
  SummaryResponse,
  TimeseriesBucket,
  TimeseriesResponse,
} from './types'

const MOCK = import.meta.env.VITE_MOCK === '1'
const TOKEN_KEY = 'aum_admin_token'

let authToken: string | null = typeof localStorage !== 'undefined' ? localStorage.getItem(TOKEN_KEY) : null
let unauthorizedHandler: (() => void) | null = null

export function getStoredToken(): string | null {
  return authToken
}

export function setStoredToken(token: string): void {
  authToken = token
  localStorage.setItem(TOKEN_KEY, token)
}

export function clearStoredToken(): void {
  authToken = null
  localStorage.removeItem(TOKEN_KEY)
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

interface RequestOptions {
  method?: 'GET' | 'POST' | 'DELETE'
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

export interface RangeParams {
  from?: string
  to?: string
  tz?: string
}

export function getSummary(params: RangeParams): Promise<SummaryResponse> {
  return request('/api/v1/summary', { params: params as Params })
}

export function getTimeseries(params: RangeParams & { bucket?: TimeseriesBucket; group_by?: GroupBy }): Promise<TimeseriesResponse> {
  return request('/api/v1/timeseries', { params: params as Params })
}

export function getBreakdown(
  params: RangeParams & { group_by: BreakdownGroupBy; source?: string; device?: string; limit?: number },
): Promise<BreakdownResponse> {
  return request('/api/v1/breakdown', {
    params: { ...params, limit: params.limit !== undefined ? String(params.limit) : undefined } as Params,
  })
}

export function getEvents(
  params: RangeParams & { source?: string; device?: string; model?: string; limit?: number; cursor?: string },
): Promise<EventsResponse> {
  return request('/api/v1/events', {
    params: { ...params, limit: params.limit !== undefined ? String(params.limit) : undefined } as Params,
  })
}

export function getDevices(): Promise<DevicesResponse> {
  return request('/api/v1/devices')
}

export function createDevice(name: string): Promise<CreateDeviceResponse> {
  return request('/api/v1/devices', { method: 'POST', body: { name } })
}

export function deleteDevice(id: string): Promise<void> {
  return request(`/api/v1/devices/${encodeURIComponent(id)}`, { method: 'DELETE' })
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
