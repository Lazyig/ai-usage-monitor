import { expect, test } from '@playwright/test'
import { prepare } from './support'

test('Codex tiers filter models and events without changing model names', async ({ page, request }, testInfo) => {
  const headers = { Authorization: 'Bearer review-only-token' }
  const name = `review-tier-${testInfo.project.name}`
  const created = await request.post('/api/v1/devices', { headers, data: { name } })
  expect(created.ok()).toBe(true)
  const device = await created.json()
  try {
    const events = ['', 'default', 'priority'].map((tier, i) => ({
      event_id: `${device.id}-tier-${i}`, device_id: device.id, source: 'codex',
      ts: '2026-10-22T08:00:00Z', model: 'gpt-6.1-sol', service_tier: tier || undefined,
      input: 1000, output: 100, cache_read: 0, cache_write: 0, cache_write_5m: 0, cache_write_1h: 0,
      reasoning: 0, confidence: 'exact',
    }))
    const ingested = await request.post('/api/v1/ingest', { headers: { Authorization: `Bearer ${device.token}` }, data: { device: { id: device.id }, events } })
    expect(ingested.ok()).toBe(true)
    expect((await ingested.json()).accepted).toBe(3)
    await prepare(page)
    const filteredEndpoints = new Set<string>()
    page.on('request', (req) => {
      const url = new URL(req.url())
      if (url.searchParams.get('service_tier') === 'fast') filteredEndpoints.add(url.pathname)
    })
    await page.goto('/models')
    await expect(page.getByText('Codex 서비스 등급별 사용량', { exact: true })).toBeVisible()
    await page.getByLabel('서비스 등급 (선택 설정)', { exact: true }).click()
    await page.getByRole('option', { name: 'Fast', exact: true }).click()
    await expect.poll(() => [...filteredEndpoints].sort()).toEqual(['/api/v1/breakdown', '/api/v1/throughput', '/api/v1/timeseries'])
    await expect(page.getByRole('row', { name: /^gpt-6\.1-sol Codex / })).toBeVisible()
    await expect(page.getByText(/비용은 Standard API 단가 기준 환산액/)).toBeVisible()

    await page.goto('/events')
    await page.getByLabel('기기', { exact: true }).click()
    await page.getByRole('option', { name, exact: true }).click()
    await expect(page.locator('tbody tr')).toHaveCount(3)
    for (const [label, tier] of [['Fast', 'fast'], ['Standard', 'default'], ['알 수 없음', 'unknown']]) {
      await page.getByLabel('서비스 등급 (선택 설정)', { exact: true }).click()
      const response = page.waitForResponse((r) => new URL(r.url()).pathname === '/api/v1/events' && new URL(r.url()).searchParams.get('service_tier') === tier)
      await page.getByRole('option', { name: label, exact: true }).click()
      expect((await response).ok()).toBe(true)
      await expect(page.locator('tbody tr')).toHaveCount(1)
      await expect(page.locator('tbody tr').getByText('gpt-6.1-sol', { exact: true })).toBeVisible()
      await expect(page.locator('tbody tr').getByText(label, { exact: true })).toBeVisible()
    }
  } finally {
    await request.delete(`/api/v1/devices/${device.id}?purge=true`, { headers })
  }
})
