import { WebRoute } from '@/lib/routes'
import { expect, test } from './fixtures'
import { login } from './helpers'

test('users: add, edit role, delete', async ({ page }) => {
  await login(page)

  // Navigate to Users
  await page.goto(WebRoute('users'))
  await expect(page.getByRole('heading', { name: /Users/i })).toBeVisible()

  const timestamp = Date.now()
  const email = `e2e-web-user-${timestamp}@example.com`

  // Add user
  await page.getByRole('button', { name: /Add User/i }).click()
  await page.getByLabel('Email').fill(email)
  await page.getByLabel('Name').fill(`E2E Web User ${timestamp}`)
  // Role defaults to viewer; save
  await page.getByRole('button', { name: /Add User/i }).click()

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

  // Edit role to admin
  await row.getByRole('button', { name: /Edit User/i }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('radio', { name: /^Admin\b/i }).check()
  await dialog.getByRole('button', { name: /Save Changes/i }).click()
  // Role badge should show Administrator
  await expect(row.getByText('Administrator')).toBeVisible()

  // Delete user (confirm dialog)
  await row.getByRole('button', { name: /Delete User/i }).click()
  const deleteDialog = page.getByRole('dialog')
  await expect(deleteDialog).toBeVisible()
  await deleteDialog.getByLabel('Confirmation', { exact: false }).fill('DELETE')
  await deleteDialog.getByRole('button', { name: /Delete User/i }).click()
  await expect(page.getByText('User deleted successfully', { exact: true })).toBeVisible()
  await expect(row).toHaveCount(0)
})

test('current user footer link opens profile page', async ({ page }) => {
  await login(page)

  // Ensure layout loaded before interacting with footer link
  await page.goto(WebRoute('rack'))
  await expect(page.getByRole('heading', { name: /Rack/i })).toBeVisible()

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

  // Add user
  await page.getByRole('button', { name: /Add User/i }).click()
  await page.getByLabel('Email').fill(email)
  await page.getByLabel('Name').fill(`E2E Web User ${timestamp}`)
  await page.getByRole('button', { name: /Add User/i }).click()

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

  // Delete user to keep DB clean between runs
  await row.getByRole('button', { name: /Delete User/i }).click()
  const deleteDialog = page.getByRole('dialog')
  await expect(deleteDialog).toBeVisible()
  await deleteDialog.getByLabel('Confirmation', { exact: false }).fill('DELETE')
  await deleteDialog.getByRole('button', { name: /Delete User/i }).click()
  await expect(page.getByText('User deleted successfully', { exact: true })).toBeVisible()
  await expect(row).toHaveCount(0)
})

test('tokens: create, rename, delete', async ({ page }) => {
  await login(page)

  // Navigate to API Tokens
  await page.goto(WebRoute('api_tokens'))
  await expect(page.getByRole('heading', { name: /API Tokens/i })).toBeVisible()

  const timestamp = Date.now()
  const name1 = `E2E Web API Token ${timestamp}`

  // Create token
  await page.getByRole('button', { name: /Create Token/i }).click()
  await page.getByLabel('Token Name').fill(name1)
  await page.getByRole('button', { name: /Create Token/i }).click()
  // Close created token dialog
  await page.getByRole('button', { name: /Done/i }).click()

  // Verify row appears
  const row = page.locator('tr', { hasText: name1 })
  await expect(row).toBeVisible()

  // Rename token with explicit aria label
  await row.getByRole('button', { name: /Edit Token/i }).click()
  const name2 = `${name1} Renamed`
  await page.getByLabel('Token Name').fill(name2)
  await page.getByRole('button', { name: /^Save$/ }).click()
  await expect(page.locator('tr', { hasText: name2 })).toBeVisible()

  // Delete token
  const row2 = page.locator('tr', { hasText: name2 })
  // Delete token using aria label
  await row2.getByRole('button', { name: /Delete Token/i }).click()
  // Confirm modal: type DELETE then confirm
  const confirmDialog = page.getByRole('dialog')
  await confirmDialog.getByLabel('Confirmation').fill('DELETE')
  await confirmDialog.getByRole('button', { name: /Delete Token/i }).click()
  await expect(row2).toHaveCount(0)
})

test('tokens: name length validation', async ({ page }) => {
  await login(page)

  await page.goto(WebRoute('api_tokens'))
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
  await page.goto(WebRoute('api_tokens'))
  await expect(page.getByRole('heading', { name: /API Tokens/i })).toBeVisible()
  const timestamp = Date.now()
  const tokenName = `E2E Web API Token ${timestamp}`
  await page.getByRole('button', { name: /Create Token/i }).click()
  await page.getByLabel('Token Name').fill(tokenName)
  await page.getByRole('button', { name: /Create Token/i }).click()
  await expect(page.getByText('API token created successfully', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: /Done/i }).click()

  const tokenCell = page.locator('table tbody td', { hasText: tokenName })
  await expect(tokenCell).toBeVisible()

  // Navigate to Audit Logs
  await page.goto(WebRoute('audit_logs'))
  await expect(page.getByRole('heading', { name: /Audit Logs/i })).toBeVisible()

  // Ensure table rendered
  const table = page.getByRole('table')
  await expect(table).toBeVisible()

  // Filter by Action Type: Token Management
  await page.locator('#action-type').click()
  await page.getByRole('option', { name: /Token Management/i }).click()

  // Filter by Status: Success
  await page.locator('#status').click()
  await page.getByRole('option', { name: /Success/i }).click()

  // Search for the created token name
  await page.getByLabel('Search').fill(tokenName)

  const filteredActionCell = page.locator('table tbody td', { hasText: 'api_token.create' }).first()
  await expect(filteredActionCell).toBeVisible()

  // name is truncated to fit in the cell
  const filteredResourceCell = page.locator('table tbody td', { hasText: 'E2E Web API' }).first()
  await expect(filteredResourceCell).toBeVisible()

  // Clean up token to avoid test fixture buildup
  await page.goto(WebRoute('api_tokens'))
  const row = page.locator('tr', { hasText: tokenName })
  await expect(row).toBeVisible()
  await row.getByRole('button', { name: /Delete Token/i }).click()
  const confirmDialog = page.getByRole('dialog')
  await confirmDialog.getByLabel('Confirmation').fill('DELETE')
  await confirmDialog.getByRole('button', { name: /Delete Token/i }).click()
  await expect(row).toHaveCount(0)
})
