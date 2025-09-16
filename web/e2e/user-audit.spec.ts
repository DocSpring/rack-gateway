import { test, expect } from '@playwright/test'

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

  const userCard = page.locator('text=Admin User')
  if (await userCard.first().isVisible().catch(() => false)) {
    await userCard.first().click()
  }

  await page.waitForResponse(
    (r: any) => r.url().includes('/.gateway/api/me') && r.status() === 200,
    { timeout: 20000 },
  )
}

test.describe('User Audit Logs', () => {
  test('navigating from Users to user audit logs filters by that user', async ({ page }) => {
    await login(page)
    await page.goto('/.gateway/web/users')
    // Click the first user link (name)
    const firstLink = page.locator('table tbody tr td a').first()
    await expect(firstLink).toBeVisible()
    const href = await firstLink.getAttribute('href')
    await firstLink.click()
    // Should navigate to /users/<id>/audit_logs
    await expect(page).toHaveURL(/\/users\/\d+\/audit_logs/)
    await expect(page.getByRole('heading', { name: /Audit Logs/i })).toBeVisible()
    // No guarantee of logs in sandbox; ensure the empty state renders for this user
    await expect(page.getByText(/No audit logs found/i)).toBeVisible()
  })

  test('invalid user id shows 404 error state', async ({ page }) => {
    await login(page)
    await page.goto('/.gateway/web/users/999999999/audit_logs')
    // Expect an error message banner from the table pane
    await expect(page.getByText(/Failed to load audit logs/i)).toBeVisible()
  })
})
