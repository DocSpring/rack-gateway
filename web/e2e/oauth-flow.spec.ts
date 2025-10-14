import { APIRoute, WebRoute } from '@/lib/routes'
import { expect, test } from './fixtures'
import { ensureMfaEnrollment } from './helpers'

test('full OAuth login flow succeeds and /info returns user', async ({ page }) => {
  // Hit login
  await page.goto(WebRoute('login'))

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
  await page.waitForResponse((r) => r.url().includes(APIRoute('info')) && r.status() === 200, {
    timeout: 20_000,
  })

  await ensureMfaEnrollment(page)

  // Navigate to the Rack page now that enrollment is complete
  await page.goto(WebRoute('rack'))
  await expect(page.getByRole('heading', { name: 'Rack', exact: true })).toBeVisible({
    timeout: 10_000,
  })

  // Navigate to Users and ensure it renders without crashing
  await page.getByRole('link', { name: /Users/i }).click()

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

  // Now the cookie should be set; /api/v1/info should return the current user
  const infoEndpoint = APIRoute('info')
  type InfoResult = {
    ok: boolean
    status: number
    data: { user?: { email?: string | null } } | null
  }
  const info = await page.evaluate<InfoResult, string>(async (endpoint) => {
    const response = await fetch(endpoint, { credentials: 'include' })
    let data: InfoResult['data'] = null
    try {
      data = await response.json()
    } catch {}
    return { ok: response.ok, status: response.status, data }
  }, infoEndpoint)
  expect(info.ok).toBeTruthy()
  expect(info.status).toBe(200)
  expect(info.data?.user?.email).toBeTruthy()
})
