import type { Page } from '@playwright/test'

export async function prepare(page: Page, authenticated = true) {
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
