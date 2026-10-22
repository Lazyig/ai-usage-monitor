// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { clearStoredToken, deleteDevice, getDevices, getStoredToken, markUnpriced, onUnauthorized, reloadPricing, setStoredToken, verifyToken } from './client'

afterEach(() => {
  clearStoredToken()
  onUnauthorized(() => {})
  vi.unstubAllGlobals()
})

it('checks a candidate token without invalidating the active session', async () => {
  setStoredToken('active-review-token')
  const unauthorized = vi.fn()
  onUnauthorized(unauthorized)
  const fetcher = vi.fn().mockResolvedValue(new Response('{"error":"unauthorized"}', { status: 401 }))
  vi.stubGlobal('fetch', fetcher)
  await expect(verifyToken('wrong-candidate')).rejects.toMatchObject({ status: 401 })
  expect(fetcher.mock.calls[0][1].headers.Authorization).toBe('Bearer wrong-candidate')
  expect(unauthorized).not.toHaveBeenCalled()
  expect(getStoredToken()).toBe('active-review-token')
})

it('invalidates the session when an authenticated API request returns 401', async () => {
  const unauthorized = vi.fn()
  onUnauthorized(unauthorized)
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{"error":"expired"}', { status: 401 })))
  await expect(getDevices()).rejects.toMatchObject({ status: 401, message: 'expired' })
  expect(unauthorized).toHaveBeenCalledOnce()
})

it('accepts an empty revoke response and the pricing reload acknowledgement', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(new Response(null, { status: 204 }))
    .mockResolvedValueOnce(new Response('{"ok":true,"rates":57}')))
  await expect(deleteDevice('review/id')).resolves.toBeUndefined()
  await expect(reloadPricing()).resolves.toEqual({ ok: true, rates: 57 })
})

describe('markUnpriced', () => {
  it('turns a row with nothing priced into an unknown cost', () => {
    expect(markUnpriced({ cost_usd: 0, cost_known_ratio: 0 }).cost_usd).toBeNull()
  })

  it('leaves priced, partly priced and older-server rows alone', () => {
    expect(markUnpriced({ cost_usd: 3.2, cost_known_ratio: 1 }).cost_usd).toBe(3.2)
    expect(markUnpriced({ cost_usd: 3.2, cost_known_ratio: 0.4 }).cost_usd).toBe(3.2)
    expect(markUnpriced({ cost_usd: 3.2 }).cost_usd).toBe(3.2)
  })
})
