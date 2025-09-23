import { expect, test } from '@playwright/test'
import { APIRoute, WebRoute } from '@/lib/routes'
import { login } from './helpers'

function escapeRegex(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

test.describe('User Audit Logs', () => {
  test('user detail view shows audit logs filtered by that user', async ({ page }) => {
    await login(page)
    await page.goto(WebRoute('users'))
    await expect(page.getByRole('heading', { name: 'Users' })).toBeVisible()

    const timestamp = Date.now()
    const targetEmail = `e2e-user-audit-${timestamp}@example.com`
    const targetName = `E2E Audit User ${timestamp}`

    await page.getByRole('button', { name: /Add User/i }).click()
    await page.getByLabel('Email').fill(targetEmail)
    await page.getByLabel('Name').fill(targetName)
    await page.getByRole('button', { name: /Add User/i }).click()
    await expect(page.locator('text=User created successfully').first()).toBeVisible()

    const userRow = page.locator('table tbody tr', { hasText: targetEmail }).first()
    await expect(userRow).toBeVisible()

    const encodedEmail = encodeURIComponent(targetEmail)

    const auditRequestPromise = page.waitForRequest((req) => {
      if (!req.url().includes(APIRoute('admin/audit')) || req.method() !== 'GET') {
        return false
      }
      try {
        const url = new URL(req.url())
        return url.searchParams.get('user') === targetEmail
      } catch {
        return false
      }
    })

    const userLink = userRow.locator('a').first()

    await Promise.all([
      auditRequestPromise,
      page.waitForURL(new RegExp(`/users/${escapeRegex(encodedEmail)}(?:/)?$`)),
      userLink.click(),
    ])

    const auditRow = page.locator('table tbody tr', { hasText: targetEmail }).first()
    const emptyState = page.locator('text=No audit logs for this user').first()
    if ((await auditRow.count()) > 0) {
      await expect(auditRow).toBeVisible()
      await expect(auditRow).toContainText(targetEmail)
    } else {
      await expect(emptyState).toBeVisible()
    }

    // Clean up the test user to keep fixture tidy
    await page.goto(WebRoute('users'))
    const cleanupRow = page.locator('table tbody tr', { hasText: targetEmail }).first()
    await expect(cleanupRow).toBeVisible()
    await cleanupRow.getByRole('button', { name: /Delete User/i }).click()
    const deleteDialog = page.getByRole('dialog')
    await deleteDialog.getByLabel('Confirmation', { exact: false }).fill('DELETE')
    await deleteDialog.getByRole('button', { name: /Delete User/i }).click()
    await expect(cleanupRow).toHaveCount(0)
  })

  test('invalid user email shows error state', async ({ page }) => {
    await login(page)
    const missingEmail = encodeURIComponent('missing-user@example.com')
    await page.goto(WebRoute(`users/${missingEmail}`))
    await expect(page.getByRole('heading', { name: 'User' })).toBeVisible()
    await expect(page.getByText(/Unable to load user/i)).toBeVisible()
  })
})
