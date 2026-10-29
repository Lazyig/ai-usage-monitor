import { expect, type Page } from '@playwright/test'

export async function prepare(page: Page, authenticated = true, resetPreferences = authenticated) {
  // Dashboard selections live on the server: every test starts from the defaults.
  if (resetPreferences) {
    await page.request.put('/api/v1/preferences', { headers: { Authorization: 'Bearer review-only-token' }, data: {} })
  }
  await page.addInitScript(({ authenticated }) => {
    const RealDate = Date
    const now = RealDate.parse('2026-10-23T00:00:00Z')
    // Freeze calendar/relative labels without suspending animations or timers.
    class FixedDate extends RealDate {
      constructor(value?: string | number) { super(value === undefined ? now : value) }
      static now() { return now }
    }
    Object.defineProperty(window, 'Date', { value: FixedDate })
    if (authenticated) localStorage.setItem('aum_admin_token', 'review-only-token')
  }, { authenticated })
}

/** Waits for a closing menu to finish: a click on a trigger while the last
 * menu still animates out does not open the next one. */
export async function noOpenMenu(page: Page) {
  await expect(page.getByRole('menu')).toHaveCount(0)
}

/** A period button of the top bar's selector. */
export function period(page: Page, label: string) {
  return page.getByRole('group', { name: '기간 선택' }).getByRole('button', { name: label, exact: true })
}

const MORE = '기간·설정 더 보기'

/** Picks a date preset: its button when it shows one, else from its group's ▾
 * menu (named by menu), else from the "⋯" menu of a narrow bar. */
export async function selectPreset(page: Page, label: string, menu?: string) {
  await expect(page.getByRole('group', { name: '기간 선택' })).toBeVisible()
  const button = period(page, label)
  if (await button.isVisible()) return button.click()
  await noOpenMenu(page)
  const opener = menu ? page.getByRole('button', { name: menu, exact: true }) : null
  if (opener && (await opener.isVisible())) await opener.click()
  else await page.getByRole('button', { name: MORE }).click()
  await page.getByRole('menuitemcheckbox', { name: label }).first().click()
}

/** Whether automatic refresh is on, read from the switch or the "⋯" menu. */
export async function autoRefreshOn(page: Page): Promise<boolean> {
  await expect(page.getByRole('group', { name: '기간 선택' })).toBeVisible()
  const sw = page.getByRole('switch', { name: '자동 새로고침 (60초)' })
  if (await sw.isVisible()) return (await sw.getAttribute('aria-checked')) === 'true'
  await noOpenMenu(page)
  await page.getByRole('button', { name: MORE }).click()
  const on = (await page.getByRole('menuitemcheckbox', { name: '자동 새로고침 (60초)' }).getAttribute('aria-checked')) === 'true'
  await page.keyboard.press('Escape')
  await noOpenMenu(page)
  return on
}

/** Turns automatic refresh on or off, with the switch or the "⋯" menu entry. */
export async function setAutoRefresh(page: Page, on: boolean) {
  await expect(page.getByRole('group', { name: '기간 선택' })).toBeVisible()
  const sw = page.getByRole('switch', { name: '자동 새로고침 (60초)' })
  if (await sw.isVisible()) {
    await (on ? sw.check() : sw.uncheck())
    return
  }
  if ((await autoRefreshOn(page)) === on) return
  await page.getByRole('button', { name: MORE }).click()
  await page.getByRole('menuitemcheckbox', { name: '자동 새로고침 (60초)' }).click()
  expect(await autoRefreshOn(page)).toBe(on)
}
