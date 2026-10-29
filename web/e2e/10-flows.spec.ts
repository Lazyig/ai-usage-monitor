import { expect, test } from '@playwright/test'
import { prepare, selectPreset, setAutoRefresh } from './support'

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
  await selectPreset(page, '최근 7일', '최근 기간')
  await expect(page.getByRole('tab', { name: '최근 7일', exact: true })).toHaveAttribute('aria-selected', 'true')
  await selectPreset(page, '직접 선택')
  await page.getByLabel('시작일', { exact: true }).fill('2026-10-22')
  await page.getByLabel('종료일', { exact: true }).fill('2026-10-23')
  const response = page.waitForResponse((r) => r.url().includes('/api/v1/summary?') && r.url().includes('from=2026-10-22') && r.url().includes('to=2026-10-24'))
  await page.getByRole('button', { name: '적용', exact: true }).click()
  expect((await response).ok()).toBe(true)
  await setAutoRefresh(page, true)
  await setAutoRefresh(page, false)
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

test('filter dropdowns and the date picker calendar work by pointer and keyboard', async ({ page }) => {
  await prepare(page)
  await page.goto('/events')
  await expect(page.locator('tbody tr')).toHaveCount(100)
  let response = page.waitForResponse((r) => r.url().includes('/api/v1/events?') && r.url().includes('source=codex'))
  await page.getByLabel('소스').click()
  await page.getByRole('option', { name: 'Codex', exact: true }).click()
  expect((await response).ok()).toBe(true)
  await expect(page.getByLabel('소스')).toHaveText('Codex')
  await page.waitForLoadState('networkidle')
  response = page.waitForResponse((r) => r.url().includes('/api/v1/events?') && !r.url().includes('source='))
  await page.getByLabel('소스').focus()
  await page.keyboard.press('Enter')
  await expect(page.getByRole('option', { name: 'Codex', exact: true })).toBeFocused()
  await page.keyboard.press('Home')
  await expect(page.getByRole('option', { name: '전체', exact: true })).toBeFocused()
  await page.keyboard.press('Enter')
  expect((await response).ok()).toBe(true)
  await expect(page.getByLabel('소스')).toHaveText('전체')

  await page.goto('/')
  await page.getByRole('tab', { name: '직접 선택', exact: true }).click()
  await page.getByLabel('시작일', { exact: true }).fill('2026-10-22')
  await page.getByRole('button', { name: '시작일 달력 열기' }).click()
  const calendar = page.getByRole('dialog', { name: '시작일 달력' })
  await expect(calendar.getByRole('gridcell', { name: '10월 22일' })).toBeFocused()
  await page.keyboard.press('ArrowLeft')
  await page.keyboard.press('Enter')
  await expect(calendar).toHaveCount(0)
  await expect(page.getByLabel('시작일', { exact: true })).toHaveValue('2026-10-21')
  await page.getByRole('button', { name: '종료일 달력 열기' }).click()
  const endCalendar = page.getByRole('dialog', { name: '종료일 달력' })
  await expect(endCalendar.getByRole('gridcell', { name: '10월 20일' })).toBeDisabled()
  await endCalendar.getByRole('gridcell', { name: '10월 22일' }).click()
  await expect(page.getByLabel('종료일', { exact: true })).toHaveValue('2026-10-22')
  response = page.waitForResponse((r) => r.url().includes('/api/v1/summary?') && r.url().includes('from=2026-10-21') && r.url().includes('to=2026-10-23'))
  await page.getByRole('button', { name: '적용', exact: true }).click()
  expect((await response).ok()).toBe(true)
})

test('on phones the dropdown and calendar open by tap and fit the screen', async ({ page, isMobile }) => {
  test.skip(!isMobile, 'phone layout only')
  await prepare(page)
  const noHorizontalScroll = () => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)
  await page.goto('/events')
  await expect(page.locator('tbody tr')).toHaveCount(100)
  await page.getByLabel('기기').tap()
  const listbox = page.getByRole('listbox')
  await expect(listbox).toBeVisible()
  const viewport = page.viewportSize()!
  const box = (await listbox.boundingBox())!
  expect(box.x).toBeGreaterThanOrEqual(0)
  expect(box.x + box.width).toBeLessThanOrEqual(viewport.width)
  expect(await noHorizontalScroll()).toBe(true)
  await page.getByRole('option', { name: '리뷰 노트북', exact: true }).tap()
  await expect(page.getByLabel('기기')).toHaveText('리뷰 노트북')
  await expect(listbox).toHaveCount(0)

  await page.goto('/')
  await page.getByRole('tab', { name: '직접 선택', exact: true }).tap()
  await page.getByRole('button', { name: '종료일 달력 열기' }).tap()
  const calendar = page.getByRole('dialog', { name: '종료일 달력' })
  await expect(calendar).toBeVisible()
  const cal = (await calendar.boundingBox())!
  expect(cal.x).toBeGreaterThanOrEqual(0)
  expect(cal.x + cal.width).toBeLessThanOrEqual(viewport.width)
  expect(await noHorizontalScroll()).toBe(true)
  await calendar.getByRole('button', { name: '이전 달' }).tap()
  await expect(calendar).toContainText('2026년 9월')
  await calendar.getByRole('gridcell', { name: '9월 30일' }).tap()
  await expect(page.getByLabel('종료일', { exact: true })).toHaveValue('2026-09-30')
})

test('the navigation names the server build', async ({ page }) => {
  await prepare(page)
  await page.goto('/')
  if (await page.getByRole('button', { name: '메뉴 열기' }).isVisible()) {
    await page.getByRole('button', { name: '메뉴 열기' }).click()
  }
  const build = page.getByTestId('server-build')
  await expect(build).toContainText('서버 ')
  await expect(build).toHaveAttribute('title', /버전 .+/)
})

test('the selected period and auto refresh come back from the server in a fresh browser', async ({ page, browser }, testInfo) => {
  await prepare(page)
  await page.goto('/')
  const saved = page.waitForRequest((r) => r.url().endsWith('/api/v1/preferences') && r.method() === 'PUT')
  await selectPreset(page, '어제', '지난 날짜')
  await saved
  await setAutoRefresh(page, true)
  await page.waitForRequest((r) => r.url().endsWith('/api/v1/preferences') && r.method() === 'PUT')

  // Another browser: nothing in its storage but the token.
  const other = await browser.newContext({ ...testInfo.project.use, baseURL: testInfo.project.use.baseURL })
  const page2 = await other.newPage()
  await prepare(page2, true, false)
  const summary = page2.waitForRequest((r) => r.url().includes('/api/v1/summary?') && r.url().includes('to=2026-10-23'))
  await page2.goto('/')
  await summary
  await expect(page2.getByRole('tab', { name: '어제', exact: true })).toHaveAttribute('aria-selected', 'true')
  await other.close()
})

test('a narrow bar moves the least used periods into a menu', async ({ page }) => {
  await prepare(page)
  await page.goto('/')
  const width = page.viewportSize()?.width ?? 0
  const more = page.getByRole('button', { name: '기간 더 보기' })
  if (width >= 1200) {
    await expect(more).toHaveCount(0)
    await expect(page.getByRole('tab', { name: '전체', exact: true })).toBeVisible()
    return
  }
  await expect(more).toBeVisible()
  await expect(page.getByRole('tab', { name: '오늘', exact: true })).toBeVisible()
  await expect(page.getByRole('tab', { name: '전체', exact: true })).toHaveCount(0)
  await more.click()
  await page.getByRole('menuitem', { name: '전체', exact: true }).click()
  // The selected preset is always shown as a tab.
  await expect(page.getByRole('tab', { name: '전체', exact: true })).toHaveAttribute('aria-selected', 'true')
  // Nothing overflows the page sideways.
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
})

test('a group menu picks an earlier period and the tab takes its name', async ({ page }) => {
  await prepare(page) // today is Friday 2026-10-23 in Seoul
  await page.goto('/')
  const lastWeek = page.waitForResponse((r) => r.url().includes('/api/v1/summary?') && r.url().includes('from=2026-10-12') && r.url().includes('to=2026-10-19'))
  await page.getByRole('button', { name: '지난 주', exact: true }).click()
  await expect(page.getByRole('menuitem', { name: '지난주' })).toContainText('10/12–10/18')
  await page.getByRole('menuitem', { name: '지난주' }).click()
  expect((await lastWeek).ok()).toBe(true)
  await expect(page.getByRole('tab', { name: '지난주', exact: true })).toHaveAttribute('aria-selected', 'true')
  // Back to the current week from the same menu.
  await page.getByRole('button', { name: '지난 주', exact: true }).click()
  await page.getByRole('menuitem', { name: '이번 주' }).click()
  await expect(page.getByRole('tab', { name: '이번 주', exact: true })).toHaveAttribute('aria-selected', 'true')
})
