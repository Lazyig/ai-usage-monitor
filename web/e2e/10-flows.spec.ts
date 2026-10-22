import { expect, test } from '@playwright/test'
import { prepare } from './support'

test('login rejects an invalid token, accepts the test token, and logs out', async ({ page }) => {
  await prepare(page, false)
  await page.goto('/login')
  await page.getByPlaceholder('ADMIN_TOKEN').fill('wrong-review-token')
  await page.getByRole('button', { name: '로그인', exact: true }).click()
  await expect(page.getByText('토큰이 올바르지 않습니다.')).toBeVisible()
  await page.getByPlaceholder('ADMIN_TOKEN').fill('review-only-token')
  await page.getByRole('button', { name: '로그인', exact: true }).click()
  await expect(page).toHaveURL('/')
  if (await page.getByRole('button', { name: '메뉴 열기' }).isVisible()) {
    await page.getByRole('button', { name: '메뉴 열기' }).click()
  }
  await page.getByText('로그아웃', { exact: true }).click()
  await expect(page).toHaveURL('/login')
})

test('date presets, custom dates, refresh and automatic refresh remain usable', async ({ page }) => {
  await prepare(page)
  await page.goto('/')
  await page.getByRole('tab', { name: '최근 7일', exact: true }).click()
  await expect(page.getByRole('tab', { name: '최근 7일', exact: true })).toHaveAttribute('aria-selected', 'true')
  await page.getByRole('tab', { name: '직접 선택', exact: true }).click()
  await page.getByLabel('시작일').fill('2026-10-22')
  await page.getByLabel('종료일').fill('2026-10-23')
  const response = page.waitForResponse((r) => r.url().includes('/api/v1/summary?') && r.url().includes('from=2026-10-22') && r.url().includes('to=2026-10-24'))
  await page.getByRole('button', { name: '적용', exact: true }).click()
  expect((await response).ok()).toBe(true)
  const automatic = page.getByRole('switch', { name: '자동 새로고침 (60초)' })
  await automatic.check()
  await expect(automatic).toBeChecked()
  await automatic.uncheck()
  await page.getByRole('button', { name: '새로고침', exact: true }).click()
  await expect(page.locator('h1')).toHaveText('개요')
})

test('events retain loaded pages on refresh and reset when filtered', async ({ page }) => {
  await prepare(page)
  await page.goto('/events')
  await expect(page.locator('tbody tr')).toHaveCount(100)
  await page.getByRole('button', { name: '더 보기', exact: true }).click()
  await expect(page.locator('tbody tr')).toHaveCount(200)
  await page.getByRole('button', { name: '새로고침', exact: true }).click()
  await page.waitForLoadState('networkidle')
  await expect(page.locator('tbody tr')).toHaveCount(200)
  await page.getByLabel('모델').fill('gpt-6.1-sol')
  await expect(page.locator('tbody tr')).toHaveCount(74)
  await page.getByLabel('모델').fill('')
  await expect(page.locator('tbody tr')).toHaveCount(100)
})

test('device creation, rename, resync, rotation, revocation and deletion work', async ({ page, request }, testInfo) => {
  await prepare(page)
  const initial = `review-flow-${testInfo.project.name}`
  const renamed = `${initial}-renamed`
  const headers = { Authorization: 'Bearer review-only-token' }
  try {
    await page.goto('/manage/devices')
    await page.getByLabel('이름', { exact: true }).fill(initial)
    await page.getByRole('button', { name: '추가', exact: true }).click()
    let row = page.getByRole('row').filter({ hasText: initial })
    await expect(row).toBeVisible()
    await row.hover()
    await page.getByRole('button', { name: `"${initial}" 이름 변경`, exact: true }).click()
    await page.getByLabel('새 이름').fill(renamed)
    await page.getByRole('dialog').getByRole('button', { name: '변경', exact: true }).click()
    row = page.getByRole('row').filter({ hasText: renamed })
    await expect(row).toBeVisible()
    for (const [item, confirm] of [['재동기화', '재동기화'], ['초기화', '초기화'], ['토큰 재발급', '재발급'], ['토큰 폐기', '토큰 폐기']]) {
      await row.hover()
      await page.getByRole('button', { name: `"${renamed}" 작업`, exact: true }).click()
      await page.getByRole('menuitem', { name: item, exact: true }).click()
      await page.getByRole('dialog').getByRole('button', { name: confirm, exact: true }).click()
      await expect(page.getByRole('dialog')).toHaveCount(0)
    }
    await page.getByLabel(/폐기된 기기 표시/).check()
    await expect(row).toBeVisible()
    await row.hover()
    await page.getByRole('button', { name: `"${renamed}" 작업`, exact: true }).click()
    await page.getByRole('menuitem', { name: '완전 삭제', exact: true }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByRole('button', { name: '완전 삭제', exact: true })).toBeDisabled()
    await dialog.getByRole('textbox').fill(renamed)
    await dialog.getByRole('button', { name: '완전 삭제', exact: true }).click()
    await expect(row).toHaveCount(0)
  } finally {
    // Keep later screenshots and projects on the same two-device fixture.
    const response = await request.get('/api/v1/devices', { headers })
    const { devices } = await response.json()
    for (const device of devices.filter((d: { name: string }) => [initial, renamed].includes(d.name))) {
      await request.delete(`/api/v1/devices/${device.id}?purge=true`, { headers })
    }
  }
})

test('pricing reload keeps period rows and sorting intact', async ({ page }) => {
  await prepare(page)
  const errors: string[] = []
  page.on('console', (message) => { if (message.type() === 'error') errors.push(message.text()) })
  await page.goto('/pricing')
  await expect(page.locator('tbody tr')).toHaveCount(57)
  await page.getByRole('columnheader', { name: '매칭 패턴', exact: true }).click()
  await page.getByRole('button', { name: 'pricing.yaml 다시 읽기' }).click()
  await page.waitForLoadState('networkidle')
  await expect(page.locator('tbody tr')).toHaveCount(57)
  expect(errors).toEqual([])
})
