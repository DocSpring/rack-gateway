import { WebRoute } from '@/lib/routes'
import { expect, test } from './fixtures'
import { clearStepUpSessions, login, satisfyMFAStepUpModal } from './helpers'

test('users: edit name and email with MFA always required', async ({ page }) => {
  await login(page)

  // Clear step-up sessions to force MFA prompts for this test
  await clearStepUpSessions()

  // Navigate to Users
  await page.goto(WebRoute('users'))
  await expect(page.getByRole('heading', { name: /Users/i })).toBeVisible()

  const email1 = `e2e-edit-${Date.now()}@example.com`
  const name1 = 'E2E Edit User'

  // Add user
  await page.getByRole('button', { name: /Add User/i }).click()
  await page.getByLabel('Email').fill(email1)
  await page.getByLabel('Name').fill(name1)
  await page.getByRole('button', { name: /Add User/i }).click()

  // Creating a user always requires MFA
  await satisfyMFAStepUpModal(page)

  // Verify initial row appears
  let row = page.locator('tr', { hasText: email1 })
  await expect(row).toBeVisible()
  await expect(row).toContainText(name1)

  // Open edit dialog - open dropdown and click Edit User
  await row.getByRole('button', { name: /Actions for/i }).click()
  await page.getByRole('menuitem', { name: 'Edit User' }).click()

  // Change name and email
  const email2 = `e2e-edit-${Date.now()}-updated@example.com`
  const name2 = 'E2E Edited User'
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Email').fill(email2)
  await dialog.getByLabel('Name').fill(name2)

  // Save
  await dialog.getByRole('button', { name: /Save Changes/i }).click()

  // Updating a user always requires MFA (because roles/permissions can change)
  await satisfyMFAStepUpModal(page)

  // Verify row updated to new email and name
  row = page.locator('tr', { hasText: email2 })
  await expect(row).toBeVisible()
  await expect(row).toContainText(name2)

  // Refresh and re-verify persistence
  await page.reload()
  row = page.locator('tr', { hasText: email2 })
  await expect(row).toBeVisible()
  await expect(row).toContainText(name2)

  // Delete the user - open dropdown and click Delete User
  await row.getByRole('button', { name: /Actions for/i }).click()
  await page.getByRole('menuitem', { name: 'Delete User' }).click()
  const deleteDialog = page.getByRole('dialog')
  await expect(deleteDialog).toBeVisible()
  await deleteDialog.getByLabel('Confirmation', { exact: false }).fill('DELETE')

  // Deleting a user always requires MFA
  await satisfyMFAStepUpModal(page)

  const waitForDelete = page.waitForResponse(
    (response) =>
      response.request().method() === 'DELETE' &&
      response.url().includes('/api/v1/users/') &&
      response.status() === 204
  )
  const waitForUsersReload = page.waitForResponse(
    (response) =>
      response.request().method() === 'GET' &&
      response.url().includes('/api/v1/users') &&
      response.status() === 200
  )
  await Promise.all([
    waitForDelete,
    waitForUsersReload,
    deleteDialog.getByRole('button', { name: /Delete User/i }).click(),
  ])
  await expect(page.getByText('User deleted successfully', { exact: true })).toBeVisible()
  await expect(row).toHaveCount(0)
})
