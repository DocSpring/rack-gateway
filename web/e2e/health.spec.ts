import { expect, test } from './fixtures'

test('gateway health via web proxy is OK', async ({ page }) => {
  // Hit the login page to ensure Vite dev server is accepting connections without triggering redirects
  await page.goto('/.gateway/web/login')

  // Fetch health via the browser context to avoid host resolution quirks
  const result = await page.evaluate(async () => {
    const r = await fetch('/.gateway/api/health')
    let data: any = null
    try { data = await r.json() } catch {}
    return { ok: r.ok, status: r.status, data }
  })

  expect(result.ok).toBeTruthy()
  expect(result.status).toBe(200)
  expect(result.data).toBeTruthy()
  expect(result.data.status).toBeDefined()
})
