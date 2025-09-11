import { defineConfig, devices } from '@playwright/test'

// Prefer gateway port (single-origin prod-like E2E), fall back to WEB_PORT (dev)
const port = process.env.GATEWAY_PORT || process.env.WEB_PORT || '5173'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: 'list',
  use: {
    // Use localhost to match OAuth + cookie host set by the gateway
    baseURL: `http://localhost:${port}`,
    trace: 'on-first-retry',
  },

  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],

  // Expect the dev stack to be running (task dev). No internal webServer starter.
})
