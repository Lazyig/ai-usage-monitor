import type { Page } from '@playwright/test'

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

/** Picks a date preset: its tab when it shows one, else from its group's ▾
 * menu (named by menu), else from the "⋯" menu of a narrow bar. */
export async function selectPreset(page: Page, label: string, menu?: string) {
  const tab = page.getByRole('tab', { name: label, exact: true })
  if (await tab.isVisible()) return tab.click()
  const opener = menu ? page.getByRole('button', { name: menu, exact: true }) : null
  if (opener && (await opener.isVisible())) await opener.click()
  else await page.getByRole('button', { name: '기간 더 보기' }).click()
  await page.getByRole('menuitem', { name: label }).first().click()
}

/** Turns automatic refresh on or off, from the switch or, on narrow screens, the "⋯" menu. */
export async function setAutoRefresh(page: Page, on: boolean) {
  const sw = page.getByRole('switch', { name: '자동 새로고침 (60초)' })
  if (await sw.isVisible()) return on ? sw.check() : sw.uncheck()
  await page.getByRole('button', { name: '기간 더 보기' }).click()
  await page.getByRole('menuitem', { name: '자동 새로고침 (60초)' }).click()
}
