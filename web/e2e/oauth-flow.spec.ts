import { expect, test } from './fixtures'

test('full OAuth login flow succeeds and /me returns user', async ({ page }) => {
  // Hit login
  await page.goto('/.gateway/web/login')

  const btn = page
    .getByTestId('login-cta')
    .or(page.getByRole('button', { name: /Continue with/i }))
    .or(page.getByRole('link', { name: /Continue with/i }))
  await expect(btn).toBeVisible({ timeout: 5000 })
  const navPromise = page.waitForURL(/oauth2\/v2\/auth|dev\/select-user/i)
  await btn.click()
  await navPromise

  // Navigate to OAuth selection page automatically
  await page.waitForURL(/oauth2\/v2\/auth|dev\/select-user/i)

  // If user selection page is shown, click a user card; otherwise, proceed
  const userCard = page.locator('text=Admin User')
  if (
    await userCard
      .first()
      .isVisible()
      .catch(() => false)
  ) {
    await userCard.first().click()
  }

  // After authorize, wait for the app to fetch the current user successfully
  await page.waitForResponse((r) => r.url().includes('/.gateway/api/me') && r.status() === 200, {
    timeout: 20_000,
  })

  // Land on the protected area (Users page)
  await expect(page.getByRole('heading', { name: /Users/i })).toBeVisible({ timeout: 10_000 })

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
    let data: Record<string, unknown> | null = null
    try {
      data = await r.json()
    } catch {}
    return { ok: r.ok, status: r.status, data }
  })
  expect(me.ok).toBeTruthy()
  expect(me.status).toBe(200)
  expect(me.data?.email).toBeTruthy()
})
