import { WebRoute } from '@/lib/routes'
import { getUserMfaSecret } from './db'
import { expect, test } from './fixtures'
import { clearStepUpSessions, login, satisfyMFAStepUpModal } from './helpers'

test('users: add, edit role, delete', async ({ page }) => {
  await login(page)

  // Navigate to Users
  await page.goto(WebRoute('users'))
  await expect(page.getByRole('heading', { name: /Users/i })).toBeVisible()

  const timestamp = Date.now()
  const email = `e2e-web-user-${timestamp}@example.com`
  const adminSecret = await getUserMfaSecret('admin@example.com')
  if (!adminSecret) {
    throw new Error('admin@example.com missing TOTP secret')
  }

  // Add user
  await page.getByRole('button', { name: /Add User/i }).click()
  const addDialog = page.getByRole('dialog', { name: 'Add User' })
  await expect(addDialog).toBeVisible()
  await addDialog.getByLabel('Email').fill(email)
  await addDialog.getByLabel('Name').fill(`E2E Web User ${timestamp}`)
  // Role defaults to viewer; save
  await clearStepUpSessions()
  const createUserResponse = page.waitForResponse(
    (response) =>
      response.request().method() === 'POST' &&
      response.url().includes('/api/v1/users') &&
      (response.status() === 201 || response.status() === 200)
  )
  const usersReload = page.waitForResponse(
    (response) =>
      response.request().method() === 'GET' &&
      response.url().includes('/api/v1/users') &&
      response.status() === 200
  )
  await Promise.all([
    createUserResponse,
    usersReload,
    (async () => {
      await addDialog.getByRole('button', { name: /Add User/i }).click()
      await satisfyMFAStepUpModal(page, {
        email: 'admin@example.com',
        secret: adminSecret,
        require: true,
      })
    })(),
  ])

  await expect(page.locator('text=User created successfully').first()).toBeVisible()

  // Verify row appears
  const row = page.locator('tr', { hasText: email })
  await expect(row).toBeVisible()

  // Ensure "Added By" column exists and has a value for this row
  const headers = page.locator('table thead th')
  await expect(headers.getByText(/Added By/i)).toBeVisible()
  const headerTexts = await headers.allTextContents()
  const addedByIdx = headerTexts.findIndex((t: string) => /Added By/i.test(t))
  if (addedByIdx >= 0) {
    const addedByCell = row.locator('td').nth(addedByIdx)
    await expect(addedByCell).toHaveText(/.+/)
  }

  // Edit role to admin - open dropdown and click Edit User
  await row.getByRole('button', { name: /Actions for/i }).click()
  await page.getByRole('menuitem', { name: 'Edit User' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('radio', { name: /^Admin\b/i }).check()
  await clearStepUpSessions()
  const updateRoleResponse = page.waitForResponse(
    (response) =>
      response.request().method() === 'PUT' &&
      response.url().includes(`/api/v1/users/${encodeURIComponent(email)}`)
  )
  await dialog.getByRole('button', { name: /Save Changes/i }).click()
  await satisfyMFAStepUpModal(page, {
    email: 'admin@example.com',
    secret: adminSecret,
    require: true,
  })
  await updateRoleResponse
  // Role badge should show Administrator
  await expect(row.getByText('Administrator')).toBeVisible()

  // Delete user (confirm dialog) - open dropdown and click Delete User
  await row.getByRole('button', { name: /Actions for/i }).click()
  await page.getByRole('menuitem', { name: 'Delete User' }).click()
  const deleteDialog = page.getByRole('dialog')
  await expect(deleteDialog).toBeVisible()
  await deleteDialog.getByLabel('Confirmation', { exact: false }).fill('DELETE')
  await clearStepUpSessions()
  await deleteDialog.getByRole('button', { name: /Delete User/i }).click()
  await satisfyMFAStepUpModal(page, {
    email: 'admin@example.com',
    secret: adminSecret,
    require: true,
  })
  await expect(page.getByText('User deleted successfully', { exact: true })).toBeVisible()
  await expect(row).toHaveCount(0)
})

test('current user footer link opens profile page', async ({ page }) => {
  await login(page)

  // Ensure layout loaded before interacting with footer link
  await page.goto(WebRoute('rack'))
  await expect(page.getByRole('heading', { name: 'Rack', exact: true })).toBeVisible()

  const footerLink = page.getByRole('link', { name: /Admin User\s+admin@example.com/i })
  await expect(footerLink).toBeVisible()

  const navigation = page.waitForURL(/\/users\/admin%40example\.com$/)
  await footerLink.click()
  await navigation

  await expect(page).toHaveURL(/\/users\/admin%40example\.com$/)
  await expect(page.getByTestId('user-email')).toHaveText('admin@example.com')
  await expect(page.getByRole('button', { name: /Delete User/i })).toBeVisible()
})

test('user detail view shows sessions and audit logs', async ({ page }) => {
  await login(page)

  await page.goto(WebRoute('users/admin%40example.com'))

  await expect(page.getByRole('heading', { name: /^Admin User$/ })).toBeVisible()
  await expect(page.getByTestId('user-email')).toHaveText('admin@example.com')
  await expect(page.getByText(/Unable to load user/i)).not.toBeVisible()

  const sessionsCard = page.getByTestId('user-sessions-card')
  await expect(sessionsCard).toBeVisible()
  const sessionsTable = sessionsCard.locator('table').first()
  await expect(sessionsTable).toBeVisible()
  await expect(sessionsTable.locator('tbody tr').first()).toBeVisible()

  const auditLogsCard = page.getByTestId('user-audit-logs')
  await expect(auditLogsCard).toBeVisible()
  const auditLogsTable = auditLogsCard.locator('table').first()
  await expect(auditLogsTable).toBeVisible()
  await expect(auditLogsTable.locator('tbody tr').first()).toBeVisible()
})

test('users: add shows all fields and persists after refresh', async ({ page }) => {
  await login(page)

  // Navigate to Users
  await page.goto(WebRoute('users'))
  await expect(page.getByRole('heading', { name: /Users/i })).toBeVisible()

  const timestamp = Date.now()
  const email = `e2e-web-user-${timestamp}@example.com`
  const adminSecret = await getUserMfaSecret('admin@example.com')
  if (!adminSecret) {
    throw new Error('admin@example.com missing TOTP secret')
  }

  // Add user
  await page.getByRole('button', { name: /Add User/i }).click()
  const addDialog = page.getByRole('dialog', { name: 'Add User' })
  await expect(addDialog).toBeVisible()
  await addDialog.getByLabel('Email').fill(email)
  await addDialog.getByLabel('Name').fill(`E2E Web User ${timestamp}`)
  await clearStepUpSessions()
  const createUserResponse = page.waitForResponse(
    (response) =>
      response.request().method() === 'POST' &&
      response.url().includes('/api/v1/users') &&
      (response.status() === 201 || response.status() === 200)
  )
  const usersReload = page.waitForResponse(
    (response) =>
      response.request().method() === 'GET' &&
      response.url().includes('/api/v1/users') &&
      response.status() === 200
  )
  await Promise.all([
    createUserResponse,
    usersReload,
    (async () => {
      await addDialog.getByRole('button', { name: /Add User/i }).click()
      await satisfyMFAStepUpModal(page, {
        email: 'admin@example.com',
        secret: adminSecret,
        require: true,
      })
    })(),
  ])

  await expect(page.locator('text=User created successfully').first()).toBeVisible()

  // Verify row appears with expected fields
  let row = page.locator('tr', { hasText: email })
  await expect(row).toBeVisible()

  // Refresh and ensure the row and fields persist, then validate columns
  await page.reload()
  row = page.locator('tr', { hasText: email })
  await expect(row).toBeVisible()
  // Determine column indices after reload
  const headers = page.locator('table thead th')
  const headerTexts = await headers.allTextContents()
  const createdIdx = headerTexts.findIndex((t: string) => /Created/i.test(t))
  const addedByIdx = headerTexts.findIndex((t: string) => /Added By/i.test(t))
  if (createdIdx >= 0) {
    const createdCell = row.locator('td').nth(createdIdx)
    const createdText = (await createdCell.innerText()).trim()
    expect(createdText).not.toBe('—')
    expect(createdText).not.toBe('-')
    expect(createdText.length).toBeGreaterThan(0)
  }
  if (addedByIdx >= 0) {
    const addedByCell = row.locator('td').nth(addedByIdx)
    await expect(addedByCell).toHaveText(/admin@/i)
  }

  // Delete user to keep DB clean between runs - open dropdown and click Delete User
  await row.getByRole('button', { name: /Actions for/i }).click()
  await page.getByRole('menuitem', { name: 'Delete User' }).click()
  const deleteDialog = page.getByRole('dialog')
  await expect(deleteDialog).toBeVisible()
  await deleteDialog.getByLabel('Confirmation', { exact: false }).fill('DELETE')
  await clearStepUpSessions()
  await deleteDialog.getByRole('button', { name: /Delete User/i }).click()
  await satisfyMFAStepUpModal(page, {
    email: 'admin@example.com',
    secret: adminSecret,
    require: true,
  })
  await expect(page.getByText('User deleted successfully', { exact: true })).toBeVisible()
  await expect(row).toHaveCount(0)
})

test('tokens: create, rename, delete', async ({ page }) => {
  await login(page)

  // Navigate to API Tokens
  await page.goto(WebRoute('api-tokens'))
  await expect(page.getByRole('heading', { name: /API Tokens/i })).toBeVisible()

  const timestamp = Date.now()
  const name1 = `E2E Web API Token ${timestamp}`
  const adminSecret = await getUserMfaSecret('admin@example.com')
  if (!adminSecret) {
    throw new Error('admin@example.com missing TOTP secret')
  }

  // Create token - opens create dialog
  await page.getByRole('button', { name: /Create Token/i }).click()
  const createDialog = page.getByRole('dialog')
  await expect(createDialog).toBeVisible()
  await createDialog.getByLabel('Token Name').fill(name1)

  // Submit the create token form - step-up window from login is still valid
  // so no MFA dialog will appear, token is created immediately
  const createResponse = page.waitForResponse(
    (response) =>
      response.request().method() === 'POST' && response.url().includes('/api/v1/api-tokens')
  )
  await clearStepUpSessions()
  await createDialog.getByRole('button', { name: /Create Token/i }).click()
  await satisfyMFAStepUpModal(page, {
    email: 'admin@example.com',
    secret: adminSecret,
    require: true,
  })
  await createResponse

  // Token should be created successfully and success dialog appears
  await expect(page.getByText(/API token created successfully/i)).toBeVisible()
  await page.getByRole('button', { name: /Done/i }).click()

  // Verify row appears
  const row = page.locator('tr', { hasText: name1 })
  await expect(row).toBeVisible()

  // Rename token - click dropdown and select "Edit Token"
  await row.getByRole('button', { name: /Actions for/i }).click()
  await page.getByText('Edit Token').click()
  const name2 = `${name1} Renamed`
  await page.getByLabel('Token Name').fill(name2)
  await clearStepUpSessions()
  const renameResponse = page.waitForResponse(
    (response) =>
      response.request().method() === 'PUT' && response.url().includes('/api/v1/api-tokens')
  )
  await page.getByRole('button', { name: /^Save$/ }).click()
  await satisfyMFAStepUpModal(page, {
    email: 'admin@example.com',
    secret: adminSecret,
    require: true,
  })
  await renameResponse
  await expect(page.locator('tr', { hasText: name2 })).toBeVisible()

  // Delete token - click dropdown and select "Delete Token"
  const row2 = page.locator('tr', { hasText: name2 })
  await row2.getByRole('button', { name: /Actions for/i }).click()
  await page.getByText('Delete Token').click()
  // Confirm modal: type DELETE then confirm
  const confirmDialog = page.getByRole('dialog')
  await confirmDialog.getByLabel('Confirmation').fill('DELETE')
  await clearStepUpSessions()
  const deleteResponse = page.waitForResponse(
    (response) =>
      response.request().method() === 'DELETE' && response.url().includes('/api/v1/api-tokens')
  )
  await confirmDialog.getByRole('button', { name: /Delete Token/i }).click()
  await satisfyMFAStepUpModal(page, {
    email: 'admin@example.com',
    secret: adminSecret,
    require: true,
  })
  await deleteResponse
  await expect(row2).toHaveCount(0)
})

test('tokens: name length validation', async ({ page }) => {
  await login(page)

  await page.goto(WebRoute('api-tokens'))
  await expect(page.getByRole('heading', { name: /API Tokens/i })).toBeVisible()

  await page.getByRole('button', { name: /Create Token/i }).click()

  const longName = 'X'.repeat(151)
  await page.getByLabel('Token Name').fill(longName)
  await page.getByRole('button', { name: /Create Token/i }).click()

  await expect(
    page.getByText('Token name must be 150 characters or less', { exact: true })
  ).toBeVisible()

  await page.getByRole('button', { name: /Cancel/i }).click()
})

test('audit logs: view and filter', async ({ page }) => {
  await login(page)

  // Create a token to ensure we have a recent audit entry to filter
  await page.goto(WebRoute('api-tokens'))
  await expect(page.getByRole('heading', { name: /API Tokens/i })).toBeVisible()
  const timestamp = Date.now()
  const tokenName = `E2E Web API Token ${timestamp}`
  const adminSecret = await getUserMfaSecret('admin@example.com')
  if (!adminSecret) {
    throw new Error('admin@example.com missing TOTP secret')
  }

  // Open create token dialog
  await page.getByRole('button', { name: /Create Token/i }).click()
  const createDialog = page.getByRole('dialog')
  await expect(createDialog).toBeVisible()
  await createDialog.getByLabel('Token Name').fill(tokenName)

  await clearStepUpSessions()
  const createResponse = page.waitForResponse(
    (response) =>
      response.request().method() === 'POST' && response.url().includes('/api/v1/api-tokens')
  )
  await createDialog.getByRole('button', { name: /Create Token/i }).click()
  await satisfyMFAStepUpModal(page, {
    email: 'admin@example.com',
    secret: adminSecret,
    require: true,
  })
  await createResponse

  // Token should be created successfully
  await expect(page.getByText('API token created successfully', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: /Done/i }).click()

  const tokenCell = page.locator('table tbody td', { hasText: tokenName })
  await expect(tokenCell).toBeVisible()

  // Navigate to Audit Logs
  await page.goto(WebRoute('audit-logs'))
  await expect(page.getByRole('heading', { name: /Audit Logs/i })).toBeVisible()

  // Ensure table rendered
  const table = page.getByRole('table')
  await expect(table).toBeVisible()

  // Filter by Action Type: Token Management
  await page.selectOption('#action-type', 'tokens')

  // Filter by Status: Success
  await page.selectOption('#status', 'success')

  // Search for the created token name
  await page.getByLabel('Search').fill(tokenName)

  const filteredActionCell = page.locator('table tbody td', { hasText: 'api_token.create' }).first()
  await expect(filteredActionCell).toBeVisible()

  // name is truncated to fit in the cell
  const filteredResourceCell = page.locator('table tbody td', { hasText: 'E2E Web API' }).first()
  await expect(filteredResourceCell).toBeVisible()

  // Clean up token to avoid test fixture buildup
  await page.goto(WebRoute('api-tokens'))
  const row = page.locator('tr', { hasText: tokenName })
  await expect(row).toBeVisible()
  await row.getByRole('button', { name: /Actions for/i }).click()
  await page.getByText('Delete Token').click()
  const confirmDialog = page.getByRole('dialog')
  await confirmDialog.getByLabel('Confirmation').fill('DELETE')
  await clearStepUpSessions()
  await confirmDialog.getByRole('button', { name: /Delete Token/i }).click()
  await satisfyMFAStepUpModal(page, {
    email: 'admin@example.com',
    secret: adminSecret,
    require: true,
  })
  await expect(row).toHaveCount(0)
})
