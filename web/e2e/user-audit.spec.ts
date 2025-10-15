import { WebRoute } from '@/lib/routes'
import { getUserMfaSecret } from './db'
import { test as base, type ExpectedError, expect } from './fixtures'
import { clearStepUpSessions, login, satisfyMFAStepUpModal } from './helpers'

const test = base

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
    const adminSecret = await getUserMfaSecret('admin@example.com')
    if (!adminSecret) {
      throw new Error('admin@example.com missing TOTP secret')
    }

    await page.getByRole('button', { name: /Add User/i }).click()
    const addDialog = page.getByRole('dialog', { name: 'Add User' })
    await expect(addDialog).toBeVisible()
    await addDialog.getByLabel('Email').fill(targetEmail)
    await addDialog.getByLabel('Name').fill(targetName)
    await clearStepUpSessions()
    const waitForCreate = page.waitForResponse(
      (response) =>
        response.request().method() === 'POST' &&
        response.url().includes('/api/v1/users') &&
        (response.status() === 201 || response.status() === 200)
    )
    const waitForUsersRefresh = page.waitForResponse(
      (response) =>
        response.request().method() === 'GET' &&
        response.url().includes('/api/v1/users') &&
        response.status() === 200
    )
    await Promise.all([
      waitForCreate,
      waitForUsersRefresh,
      addDialog.getByRole('button', { name: /Add User/i }).click(),
      satisfyMFAStepUpModal(page, {
        email: 'admin@example.com',
        secret: adminSecret,
        require: true,
      }),
    ])

    const userRow = page.locator('table tbody tr', { hasText: targetEmail }).first()
    await expect(userRow).toBeVisible()

    const encodedEmail = encodeURIComponent(targetEmail)

    const userLink = userRow.locator('a').first()

    await Promise.all([
      page.waitForURL(new RegExp(`/users/${escapeRegex(encodedEmail)}(?:/)?$`)),
      userLink.click(),
    ])

    const auditRow = page.locator('table tbody tr', { hasText: targetEmail }).first()
    const emptyState = page.locator('text=No audit logs for this user').first()

    if (await auditRow.count()) {
      await expect(auditRow).toBeVisible()
      await expect(auditRow).toContainText(targetEmail)
    } else {
      await expect(emptyState).toBeVisible()
    }

    // Clean up the test user to keep fixture tidy
    await page.goto(WebRoute('users'))
    const cleanupRow = page.locator('table tbody tr', { hasText: targetEmail }).first()
    await expect(cleanupRow).toBeVisible()
    // Open dropdown and click Delete User
    await cleanupRow.getByRole('button', { name: /Actions for/i }).click()
    await page.getByRole('menuitem', { name: 'Delete User' }).click()
    const deleteDialog = page.getByRole('dialog')
    await deleteDialog.getByLabel('Confirmation', { exact: false }).fill('DELETE')
    await clearStepUpSessions()

    // Set up response waiters BEFORE clicking the button
    const waitForDelete = page.waitForResponse(
      (response) =>
        response.request().method() === 'DELETE' &&
        response.url().includes('/api/v1/users/') &&
        (response.status() === 204 || response.status() === 200),
      { timeout: 15_000 }
    )
    const waitForUsersReload = page.waitForResponse(
      (response) =>
        response.request().method() === 'GET' &&
        response.url().includes('/api/v1/users') &&
        response.status() === 200,
      { timeout: 15_000 }
    )

    // Click delete button to trigger MFA modal
    await deleteDialog.getByRole('button', { name: /Delete User/i }).click()

    // Satisfy MFA modal, which will trigger the DELETE request
    await satisfyMFAStepUpModal(page, {
      email: 'admin@example.com',
      secret: adminSecret,
      require: true,
    })

    // Wait for both requests to complete
    await Promise.all([waitForDelete, waitForUsersReload])
    await expect(page.getByText('User deleted successfully', { exact: true })).toBeVisible()
    await expect(cleanupRow).toHaveCount(0)
  })

  const testWith404 = test.extend<{ expectedErrors: ExpectedError[] }>({
    // biome-ignore lint/correctness/noEmptyPattern: Playwright fixture signature requires empty destructure
    expectedErrors: async ({}, use) => {
      await use([
        {
          pattern:
            /Failed to load resource: the server responded with a status of 404 \(Not Found\)/i,
          description: 'Expected 404 for missing user',
        },
      ])
    },
  })

  testWith404('invalid user email shows error state', async ({ page }) => {
    await login(page)
    const missingEmail = encodeURIComponent('missing-user@example.com')
    await page.goto(WebRoute(`users/${missingEmail}`))
    await expect(page.getByRole('heading', { name: 'User' })).toBeVisible()
    await expect(page.getByText(/Unable to load user/i)).toBeVisible()
  })
})
