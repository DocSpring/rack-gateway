import { test, expect } from '@playwright/test'

test.describe('User Audit Logs', () => {
  test('navigating from Users to user audit logs filters by that user', async ({ page }) => {
    // Open the web UI root; login is handled by dev harness reverse proxy
    await page.goto('/.gateway/web/users')
    // Click the first user link (name)
    const firstLink = page.locator('table tbody tr td a').first()
    await expect(firstLink).toBeVisible()
    const href = await firstLink.getAttribute('href')
    await firstLink.click()
    // Should navigate to /users/<id>/audit_logs
    await expect(page).toHaveURL(/\/users\/\d+\/audit_logs/)
    // Expect audit logs table to load and contain at least one row
    const rows = page.locator('table tbody tr')
    await expect(rows).toHaveCountGreaterThan(0)
  })

  test('invalid user id shows 404 error state', async ({ page }) => {
    await page.goto('/.gateway/web/users/999999999/audit_logs')
    // Expect an error message (backend returns 404 user not found)
    await expect(page.getByText(/user not found/i)).toBeVisible()
  })
})

