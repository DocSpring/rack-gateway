import { expect, test } from '@playwright/test'

const WEB_PORT = process.env.WEB_PORT || '5173'
const BASE = `http://127.0.0.1:${WEB_PORT}`

test('login button triggers gateway OAuth redirect', async ({ page }) => {
  await page.goto(`${BASE}/login`)

  const [resp] = await Promise.all([
    page.waitForResponse((r) => r.url().includes('/.gateway/web/login')),
    page.getByRole('button', { name: /Continue with (Mock OAuth|Google)/i }).click(),
  ])

  // Expect a redirect response from the gateway
  const status = resp.status()
  expect(status).toBeGreaterThanOrEqual(300)
  expect(status).toBeLessThan(400)

  const location = resp.headers()['location'] || resp.headers()['Location']
  expect(location).toBeTruthy()
  expect(location).toMatch(/oauth2\//i)
})
