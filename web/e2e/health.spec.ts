import { expect, test } from './fixtures'

test('gateway health via web proxy is OK', async ({ page }) => {
  // Hit the login page to ensure Vite dev server is accepting connections without triggering redirects
  await page.goto('/app/login')

  // Fetch health via the browser context to avoid host resolution quirks
  const result = await page.evaluate(async () => {
    const r = await fetch('/api/v1/health')
    let data: Record<string, unknown> | null = null
    try {
      data = await r.json()
    } catch {
      // Ignore JSON parse errors - not all responses are JSON
    }
    return { ok: r.ok, status: r.status, data }
  })

  expect(result.ok).toBeTruthy()
  expect(result.status).toBe(200)
  expect(result.data).toBeTruthy()
  if (!result.data) {
    throw new Error('Expected health response data to be present')
  }
  expect('status' in result.data).toBeTruthy()
})
