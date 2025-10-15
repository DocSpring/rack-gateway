import { defineConfig, devices } from '@playwright/test'

const inferredWorkers = (() => {
  if (process.env.PLAYWRIGHT_WORKERS) {
    const configured = Number.parseInt(process.env.PLAYWRIGHT_WORKERS, 10)
    if (!Number.isNaN(configured) && configured > 0) {
      return configured
    }
  }
  if (process.env.WEB_E2E_SHARDS) {
    const shards = Number.parseInt(process.env.WEB_E2E_SHARDS, 10)
    if (!Number.isNaN(shards) && shards > 0) {
      return shards
    }
  }
  // Default to 1 worker (only parallelize when explicitly configured via task web:e2e)
  return 1
})()

export default defineConfig({
  testDir: './e2e',
  fullyParallel: inferredWorkers > 1,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: inferredWorkers,
  reporter: 'list',
  globalSetup: './e2e/e2e-global-setup.ts',
  globalTeardown: './e2e/e2e-global-teardown.ts',
  use: {
    // baseURL is set per-worker by the fixture in fixtures.ts for parallel test shards
    // DO NOT set it here or it will override the per-worker baseURL
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },

  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],

  // Expect the dev stack to be running (task dev). No internal webServer starter.
})
