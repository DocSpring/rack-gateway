import { expect, test } from './fixtures'

async function login(page: any) {
  await page.goto('/.gateway/web/login')
  const btn = page
    .getByTestId('login-cta')
    .or(page.getByRole('button', { name: /Continue with/i }))
    .or(page.getByRole('link', { name: /Continue with/i }))
  await expect(btn).toBeVisible({ timeout: 5000 })
  await Promise.all([
    page.waitForNavigation({ url: /oauth2\/v2\/auth|dev\/select-user/i }),
    btn.click(),
  ])

  // Mock OAuth user selection if shown
  const userCard = page.locator('text=Admin User')
  if (await userCard.first().isVisible().catch(() => false)) {
    await userCard.first().click()
  }

  // Wait for session readiness
  await page.waitForResponse(
    (r: any) => r.url().includes('/.gateway/api/me') && r.status() === 200,
    { timeout: 20000 },
  )
}

test('visit global Processes page', async ({ page }) => {
  await login(page)
  await page.goto('/.gateway/web/processes')
  await expect(page.getByRole('heading', { name: /Processes/i })).toBeVisible()
  // Either table or empty/loading state should render without JS errors
  const table = page.getByRole('table')
  const loading = page.getByText(/Loading processes/i)
  await expect(table.or(loading)).toBeVisible()
})

test('visit global Instances page', async ({ page }) => {
  await login(page)
  await page.goto('/.gateway/web/instances')
  await expect(page.getByRole('heading', { name: /Instances/i })).toBeVisible()
  const table = page.getByRole('table')
  const loading = page.getByText(/Loading instances/i)
  await expect(table.or(loading)).toBeVisible()
})

test('visit global Builds page', async ({ page }) => {
  await login(page)
  await page.goto('/.gateway/web/builds')
  await expect(page.getByRole('heading', { name: /Builds/i })).toBeVisible()
  const table = page.getByRole('table')
  const loading = page.getByText(/Loading builds/i)
  await expect(table.or(loading)).toBeVisible()
})

test('visit global Releases page', async ({ page }) => {
  await login(page)
  await page.goto('/.gateway/web/releases')
  await expect(page.getByRole('heading', { name: /Releases/i })).toBeVisible()
  const table = page.getByRole('table')
  const loading = page.getByText(/Loading releases/i)
  await expect(table.or(loading)).toBeVisible()
})
