import { WebRoute } from '@/lib/routes'
import { getUserMfaSecret } from './db'
import { expect, test } from './fixtures'
import { clearStepUpSessions, login, satisfyMFAStepUpModal } from './helpers'

const VIEWER_EMAIL = 'viewer@example.com'
const DEPLOYER_EMAIL = 'deployer@example.com'

test.describe('Non-admin self-service', () => {
  test('viewer sees the team directory, their own profile, sessions and activity', async ({
    page,
  }) => {
    await login(page, { userCardText: 'Viewer User', email: VIEWER_EMAIL })

    await page.goto(WebRoute('users'))
    await expect(page.getByRole('heading', { name: 'Users' })).toBeVisible()
    await expect(page.getByRole('button', { name: /Add User/i })).toHaveCount(0)

    const adminRow = page.locator('table tbody tr', { hasText: 'admin@example.com' }).first()
    await expect(adminRow).toBeVisible()
    await expect(adminRow.locator('a')).toHaveCount(0)

    const ownRow = page.locator('table tbody tr', { hasText: VIEWER_EMAIL }).first()
    await ownRow.getByRole('link', { name: VIEWER_EMAIL }).click()
    await expect(page.getByTestId('user-email')).toHaveText(VIEWER_EMAIL)
    await expect(page.getByRole('button', { name: /Sign Out Everywhere/i })).toBeVisible()
    await expect(page.getByRole('button', { name: /Delete User/i })).toHaveCount(0)
    await expect(page.getByRole('button', { name: /Lock Account/i })).toHaveCount(0)
    await expect(page.getByText(/Unable to load user/i)).toHaveCount(0)

    await page.getByRole('link', { name: 'My Activity' }).click()
    await expect(page.getByRole('heading', { name: `Audit Logs: ${VIEWER_EMAIL}` })).toBeVisible()
    await expect(page.getByText(/Failed to load audit logs/i)).toHaveCount(0)
    await expect(page.getByRole('button', { name: /Export CSV/i })).toBeDisabled()
  })

  test('deployer creates and deletes their own API token', async ({ page }) => {
    await login(page, { userCardText: 'Deployer User', email: DEPLOYER_EMAIL })
    const secret = await getUserMfaSecret(DEPLOYER_EMAIL)
    if (!secret) {
      throw new Error(`${DEPLOYER_EMAIL} missing TOTP secret`)
    }

    await page.goto(WebRoute('api-tokens'))
    await expect(page.getByRole('heading', { name: /API Tokens/i })).toBeVisible()

    const name = `E2E Deployer Token ${Date.now()}`
    await page.getByRole('button', { name: /Create Token/i }).click()
    const createDialog = page.getByRole('dialog')
    await createDialog.getByLabel('Token Name').fill(name)

    const createResponse = page.waitForResponse(
      (response) =>
        response.request().method() === 'POST' && response.url().includes('/api/v1/api-tokens')
    )
    await clearStepUpSessions()
    await createDialog.getByRole('button', { name: /Create Token/i }).click()
    await satisfyMFAStepUpModal(page, { email: DEPLOYER_EMAIL, secret, require: true })
    expect((await createResponse).status()).toBe(200)
    await expect(page.getByText(/API token created successfully/i)).toBeVisible()
    await page.getByRole('button', { name: /Done/i }).click()

    const row = page.locator('tr', { hasText: name })
    await expect(row).toBeVisible()

    await row.getByRole('button', { name: /Actions for/i }).click()
    await page.getByText('Delete Token').click()
    const deleteDialog = page.getByRole('dialog')
    await deleteDialog.getByLabel('Confirmation').fill('DELETE')
    const deleteResponse = page.waitForResponse(
      (response) =>
        response.request().method() === 'DELETE' && response.url().includes('/api/v1/api-tokens')
    )
    await clearStepUpSessions()
    await deleteDialog.getByRole('button', { name: /Delete Token/i }).click()
    await satisfyMFAStepUpModal(page, { email: DEPLOYER_EMAIL, secret, require: true })
    expect((await deleteResponse).status()).toBe(204)
    await expect(row).toHaveCount(0)
  })
})
