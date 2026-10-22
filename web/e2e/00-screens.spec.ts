import { expect, test } from '@playwright/test'
import { prepare } from './support'

for (const [name, path] of [
  ['login', '/login'], ['overview', '/'], ['agents', '/agents'], ['models', '/models'],
  ['projects', '/projects'], ['device-usage', '/devices'], ['device-management', '/manage/devices'],
  ['events', '/events'], ['pricing', '/pricing'], ['downloads', '/downloads-page'],
]) {
  test(`${name}: normal rendering and optional exact screenshot`, async ({ page }) => {
    const errors: string[] = []
    page.on('pageerror', (error) => errors.push(error.message))
    await prepare(page, name !== 'login')
    await page.goto(path)
    await page.waitForLoadState('networkidle')
    await expect(page.locator('h1').first()).toBeVisible()
    await expect(page.getByText('데이터를 불러오지 못했습니다:', { exact: false })).toHaveCount(0)
    // Recharts uses JS animation; wait for its default duration to finish.
    await page.waitForTimeout(1800)
    if (process.env.REVIEW_SCREENSHOTS === '1') {
      await expect(page).toHaveScreenshot(`${name}.png`)
    }
    expect(errors).toEqual([])
  })
}
