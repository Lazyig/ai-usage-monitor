import { defineConfig } from '@playwright/test'

// The review runner uses a local server with synthetic data, never production.
const baseURL = process.env.REVIEW_BASE_URL ?? 'http://127.0.0.1:18081'
if (!['127.0.0.1', 'localhost', '[::1]'].includes(new URL(baseURL).hostname)) {
  throw new Error('Review browser tests must target a loopback server')
}

export default defineConfig({
  testDir: './e2e',
  workers: 1,
  timeout: 30_000,
  outputDir: process.env.REVIEW_OUTPUT_DIR ?? '/tmp/ai-usage-review/browser-results',
  snapshotPathTemplate: `${process.env.REVIEW_SNAPSHOT_DIR ?? '/tmp/ai-usage-review/screens'}/{projectName}/{arg}{ext}`,
  expect: { toHaveScreenshot: { maxDiffPixels: 0, animations: 'disabled' } },
  use: {
    baseURL,
    locale: 'ko-KR',
    timezoneId: 'Asia/Seoul',
    launchOptions: {
      executablePath: process.env.REVIEW_CHROMIUM_EXECUTABLE,
      args: ['--disable-dev-shm-usage'],
    },
  },
  projects: [
    { name: 'desktop-light', use: { viewport: { width: 1440, height: 1000 }, colorScheme: 'light' } },
    { name: 'desktop-dark', use: { viewport: { width: 1440, height: 1000 }, colorScheme: 'dark' } },
    { name: 'mobile-light', use: { viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, colorScheme: 'light' } },
    { name: 'mobile-dark', use: { viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, colorScheme: 'dark' } },
  ],
})
