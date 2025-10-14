import type { Page } from '@playwright/test'
import { WebRoute } from '@/lib/routes'
import { expect, test } from './fixtures'
import { clearStepUpSessions, login, satisfyMFAStepUpModal } from './helpers'

const headingLocator = { name: /Users/i }

const multiFactorDialog = (page: Page) =>
  page.getByRole('dialog', { name: /Multi-Factor Authentication Required/i })

const usersRow = (page: Page, email: string) => page.locator('tr', { hasText: email })

async function gotoUsersPage(page: Page) {
  await page.goto(WebRoute('users'))
  await expect(page.getByRole('heading', headingLocator)).toBeVisible()
}

async function createUser(page: Page, email: string, name: string) {
  await page.getByRole('button', { name: /Add User/i }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Email').fill(email)
  await dialog.getByLabel('Name').fill(name)
  await dialog.getByRole('button', { name: /Add User/i }).click()

  await satisfyMFAStepUpModal(page)

  const row = usersRow(page, email)
  await expect(row).toBeVisible()
  await expect(row).toContainText(name)
}

async function openEditDialog(page: Page, email: string) {
  const row = usersRow(page, email)
  await expect(row).toBeVisible()
  await row.getByRole('button', { name: /Actions for/i }).click()
  await page.getByRole('menuitem', { name: 'Edit User' }).click()
  const dialog = page.getByRole('dialog').filter({ hasText: /Save Changes/i })
  await expect(dialog).toBeVisible()
  return dialog
}

async function deleteUser(page: Page, email: string) {
  const row = usersRow(page, email)
  await expect(row).toBeVisible()
  await row.getByRole('button', { name: /Actions for/i }).click()
  await page.getByRole('menuitem', { name: 'Delete User' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  await dialog.getByLabel('Confirmation', { exact: false }).fill('DELETE')

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

  dialog.getByRole('button', { name: /Delete User/i }).click()
  await satisfyMFAStepUpModal(page)

  await Promise.all([waitForDelete, waitForUsersReload])

  await expect(page.getByText('User deleted successfully', { exact: true })).toBeVisible()
  await expect(row).toHaveCount(0)
}

test.describe('User editing MFA behavior', () => {
  test('edit name and email with MFA always required', async ({ page }) => {
    await login(page)
    await clearStepUpSessions()
    await gotoUsersPage(page)

    const email1 = `e2e-edit-${Date.now()}@example.com`
    const name1 = 'E2E Edit User'
    await createUser(page, email1, name1)

    await clearStepUpSessions()

    const email2 = `e2e-edit-${Date.now()}-updated@example.com`
    const name2 = 'E2E Edited User'

    const dialog = await openEditDialog(page, email1)
    await dialog.getByLabel('Email').fill(email2)
    await dialog.getByLabel('Name').fill(name2)

    const waitForUpdate = page.waitForResponse(
      (response) =>
        response.request().method() === 'PUT' &&
        response.url().includes(`/api/v1/users/${encodeURIComponent(email1)}`) &&
        response.status() === 200
    )
    await dialog.getByRole('button', { name: /Save Changes/i }).click()

    await satisfyMFAStepUpModal(page)
    await waitForUpdate

    const updatedRow = usersRow(page, email2)
    await expect(updatedRow).toBeVisible()
    await expect(updatedRow).toContainText(name2)

    await page.reload()
    await expect(usersRow(page, email2)).toContainText(name2)

    await deleteUser(page, email2)
  })

  test('edit name only uses MFA step-up window logic', async ({ page }) => {
    await login(page)
    await clearStepUpSessions()
    await gotoUsersPage(page)

    const email = `e2e-edit-name-${Date.now()}@example.com`
    const originalName = 'E2E StepUp User'
    await createUser(page, email, originalName)

    await clearStepUpSessions()

    const firstName = 'E2E StepUp User Renamed'
    let dialog = await openEditDialog(page, email)
    await dialog.getByLabel('Name').fill(firstName)
    const firstUpdate = page.waitForResponse(
      (response) =>
        response.request().method() === 'PUT' &&
        response.url().includes(`/api/v1/users/${encodeURIComponent(email)}/name`) &&
        response.status() === 200
    )
    await dialog.getByRole('button', { name: /Save Changes/i }).click()
    await satisfyMFAStepUpModal(page)
    await firstUpdate

    let row = usersRow(page, email)
    await expect(row).toContainText(firstName)

    const secondName = 'E2E StepUp User Renamed Again'
    dialog = await openEditDialog(page, email)
    await dialog.getByLabel('Name').fill(secondName)
    const secondUpdate = page.waitForResponse(
      (response) =>
        response.request().method() === 'PUT' &&
        response.url().includes(`/api/v1/users/${encodeURIComponent(email)}/name`) &&
        response.status() === 200
    )
    await dialog.getByRole('button', { name: /Save Changes/i }).click()
    await expect(multiFactorDialog(page)).toBeHidden({ timeout: 2000 })
    await secondUpdate

    row = usersRow(page, email)
    await expect(row).toContainText(secondName)

    await deleteUser(page, email)
  })

  test('edit name and role requires fresh MFA verification', async ({ page }) => {
    await login(page)
    await clearStepUpSessions()
    await gotoUsersPage(page)

    const email = `e2e-edit-role-${Date.now()}@example.com`
    const name = 'E2E Role User'
    await createUser(page, email, name)

    await clearStepUpSessions()

    const updatedName = 'E2E Role User Updated'
    const dialog = await openEditDialog(page, email)
    await dialog.getByLabel('Name').fill(updatedName)
    await dialog.getByRole('radio', { name: 'Admin' }).check()

    const waitForUpdate = page.waitForResponse(
      (response) =>
        response.request().method() === 'PUT' &&
        response.url().includes(`/api/v1/users/${encodeURIComponent(email)}`) &&
        response.status() === 200
    )
    await dialog.getByRole('button', { name: /Save Changes/i }).click()
    await satisfyMFAStepUpModal(page)
    await waitForUpdate

    const row = usersRow(page, email)
    await expect(row).toContainText(updatedName)
    await expect(row).toContainText('Administrator')

    await deleteUser(page, email)
  })
})
