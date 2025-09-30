import { defineConfig, devices } from '@playwright/test'

// Prefer gateway port (single-origin prod-like E2E), fall back to WEB_PORT (dev)
const port =
  process.env.E2E_GATEWAY_PORT ||
  process.env.TEST_GATEWAY_PORT ||
  process.env.GATEWAY_PORT ||
  process.env.WEB_PORT ||
  '5223'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: 1,
  reporter: 'list',
  globalSetup: './e2e/e2e-global-setup.ts',
  globalTeardown: './e2e/e2e-global-teardown.ts',
  use: {
    // Use localhost to match OAuth + cookie host set by the gateway
    baseURL: `http://localhost:${port}`,
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
