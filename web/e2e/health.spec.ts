import { expect, test } from '@playwright/test'

const WEB_PORT = process.env.WEB_PORT || '5173'
const BASE = `http://127.0.0.1:${WEB_PORT}`

test('gateway health via web proxy is OK', async ({ page }) => {
  // Hit the site to ensure Vite dev server is accepting connections
  await page.goto(`${BASE}/.gateway/web/`)

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
