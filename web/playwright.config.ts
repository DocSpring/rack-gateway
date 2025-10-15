import { defineConfig, devices } from '@playwright/test'

const parseList = (value?: string) =>
  value
    ? value
        .split(',')
        .map((item) => item.trim())
        .filter(Boolean)
    : []

const pickFirstNonEmpty = (...lists: string[][]) => {
  for (const list of lists) {
    if (list.length > 0) {
      return list
    }
  }
  return []
}

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

const gatewayPorts = pickFirstNonEmpty(
  parseList(process.env.E2E_GATEWAY_PORTS),
  parseList(process.env.E2E_GATEWAY_PORT),
  parseList(process.env.TEST_GATEWAY_PORT),
  parseList(process.env.GATEWAY_PORT),
  parseList(process.env.WEB_PORT)
)
const basePort = gatewayPorts[0] || process.env.TEST_GATEWAY_PORT || '9447'

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
    // Use localhost to match OAuth + cookie host set by the gateway
    baseURL: `http://localhost:${basePort}`,
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
