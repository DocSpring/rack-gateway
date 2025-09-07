import { expect, test } from '@playwright/test'

const WEB_PORT = process.env.WEB_PORT || '5173'
const BASE = `http://localhost:${WEB_PORT}`

test('full OAuth login flow succeeds and /me returns user', async ({ page }) => {
  // Hit login
await page.goto(`${BASE}/.gateway/web/login`)

  // Click login and wait for gateway login redirect
  const [loginResp] = await Promise.all([
    page.waitForResponse((r) => r.url().includes('/.gateway/api/web/login')),
    page.getByRole('button', { name: /Continue with (Mock OAuth|Google)/i }).click(),
  ])
  expect(loginResp.status()).toBeGreaterThanOrEqual(300)

  // Navigate to OAuth selection page automatically
  await page.waitForURL(/oauth2\/v2\/auth|dev\/select-user/i)

  // If user selection page is shown, click a user card; otherwise, proceed
  const userCard = page.locator('text=Admin User')
  if (await userCard.first().isVisible().catch(() => false)) {
    await userCard.first().click()
  }

  // After authorize, wait for the app to fetch the current user successfully
  await page.waitForResponse(
    (r) => r.url().includes('/.gateway/api/me') && r.status() === 200,
    { timeout: 20000 },
  )

  // Land on the protected area (Users page)
  await expect(page.getByRole('heading', { name: /Users/i })).toBeVisible({ timeout: 10000 })

  // The protected Users page should render for an authenticated admin
  await expect(page.getByRole('heading', { name: /Users/i })).toBeVisible()
  await expect(page.getByRole('button', { name: /Add User/i })).toBeVisible()

  // Navigate to API Tokens and ensure it renders without crashing
  await page.getByRole('link', { name: /API Tokens/i }).click()
  await expect(page.getByRole('heading', { name: /API Tokens/i })).toBeVisible()
  // Either empty state or a table is present
  const empty = page.getByText('No API tokens created yet')
  const table = page.getByRole('table')
  await expect(empty.or(table)).toBeVisible()

  // Now the cookie should be set; /.gateway/api/me should return the current user
  const me = await page.evaluate(async () => {
    const r = await fetch('/.gateway/api/me', { credentials: 'include' })
    let data = null
    try { data = await r.json() } catch {}
    return { ok: r.ok, status: r.status, data }
  })
  expect(me.ok).toBeTruthy()
  expect(me.status).toBe(200)
  expect(me.data?.email).toBeTruthy()
})
