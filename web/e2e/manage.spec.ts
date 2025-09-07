import { expect, test } from '@playwright/test'

const WEB_PORT = process.env.WEB_PORT || '5173'
const BASE = `http://localhost:${WEB_PORT}`

async function login(page: any) {
  await page.goto(`${BASE}/.gateway/web/login`)
  const [loginResp] = await Promise.all([
    page.waitForResponse((r: any) => r.url().includes('/.gateway/api/web/login')),
    page.getByRole('button', { name: /Continue with (Mock OAuth|Google)/i }).click(),
  ])
  expect(loginResp.status()).toBeGreaterThanOrEqual(300)

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

test('users: add, edit role, delete', async ({ page }) => {
  await login(page)

  // Navigate to Users
  await page.goto(`${BASE}/.gateway/web/users`)
  await expect(page.getByRole('heading', { name: /Users/i })).toBeVisible()

  const email = `e2e-user-${Date.now()}@company.com`

  // Add user
  await page.getByRole('button', { name: /Add User/i }).click()
  await page.getByLabel('Email').fill(email)
  await page.getByLabel('Name').fill('E2E User')
  // Role defaults to viewer; save
  await page.getByRole('button', { name: /Add User/i }).click()

  // Verify row appears
  const row = page.locator('tr', { hasText: email })
  await expect(row).toBeVisible()

  // Edit role to admin
  await row.getByRole('button', { name: /Edit User/i }).click()
  // Choose Administrator within the open dialog to avoid strict matches
  const dialog = page.getByRole('dialog')
  await dialog.getByText('Administrator').click()
  await page.getByRole('button', { name: /Update User/i }).click()
  // Role badge should show Administrator
  await expect(row.getByText('Administrator')).toBeVisible()

  // Delete user (confirm dialog)
  page.once('dialog', (d) => d.accept())
  await row.getByRole('button', { name: /Delete User/i }).click()
  await expect(row).toHaveCount(0)
})

test('tokens: create, rename, delete', async ({ page }) => {
  await login(page)

  // Navigate to API Tokens
  await page.goto(`${BASE}/.gateway/web/api_tokens`)
  await expect(page.getByRole('heading', { name: /API Tokens/i })).toBeVisible()

  const name1 = `E2E Token ${Date.now()}`

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
  await expect(row2).toHaveCount(0)
})

test('audit logs: view and filter', async ({ page }) => {
  await login(page)

  // Create a token to ensure we have a recent audit entry to filter
  await page.goto(`${BASE}/.gateway/web/api_tokens`)
  await expect(page.getByRole('heading', { name: /API Tokens/i })).toBeVisible()
  const tokenName = `E2E Audit Token ${Date.now()}`
  await page.getByRole('button', { name: /Create Token/i }).click()
  await page.getByLabel('Token Name').fill(tokenName)
  await page.getByRole('button', { name: /Create Token/i }).click()
  await page.getByRole('button', { name: /Done/i }).click()

  // Navigate to Audit Logs
  await page.goto(`${BASE}/.gateway/web/audit_logs`)
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

  // Verify at least one data row remains
  await expect(page.locator('table tbody tr').first()).toBeVisible()
})
