import { expect, test } from './fixtures'
import { login } from './helpers'

test('users: edit name and email', async ({ page }) => {
  await login(page)

  // Navigate to Users
  await page.goto('/.gateway/web/users')
  await expect(page.getByRole('heading', { name: /Users/i })).toBeVisible()

  const email1 = `e2e-edit-${Date.now()}@example.com`
  const name1 = 'E2E Edit User'

  // Add user
  await page.getByRole('button', { name: /Add User/i }).click()
  await page.getByLabel('Email').fill(email1)
  await page.getByLabel('Name').fill(name1)
  await page.getByRole('button', { name: /Add User/i }).click()

  // Verify initial row appears
  let row = page.locator('tr', { hasText: email1 })
  await expect(row).toBeVisible()
  await expect(row).toContainText(name1)

  // Open edit dialog
  await row.getByRole('button', { name: /Edit User/i }).click()

  // Change name and email
  const email2 = `e2e-edit-${Date.now()}-updated@example.com`
  const name2 = 'E2E Edited User'
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Email').fill(email2)
  await dialog.getByLabel('Name').fill(name2)

  // Save
  await dialog.getByRole('button', { name: /Save Changes/i }).click()

  // Verify row updated to new email and name
  row = page.locator('tr', { hasText: email2 })
  await expect(row).toBeVisible()
  await expect(row).toContainText(name2)

  // Refresh and re-verify persistence
  await page.reload()
  row = page.locator('tr', { hasText: email2 })
  await expect(row).toBeVisible()
  await expect(row).toContainText(name2)

  // Cleanup: delete the user
  page.once('dialog', (d) => d.accept())
  await row.getByRole('button', { name: /Delete User/i }).click()
  await expect(row).toHaveCount(0)
})
