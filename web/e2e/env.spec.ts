import { WebRoute } from '@/lib/routes'
import { expect, test } from './fixtures'
import { getUserMfaSecret } from './db'
import { clearStepUpSessions, login, satisfyMFAStepUpModal } from './helpers'

const SECRET_VALUE = 'super-secret-key-12345'

test('app environment management: reveal secret and save change', async ({ page }) => {
  await login(page)

  await page.goto(WebRoute('apps/rack-gateway/env'))
  const envTitle = page.locator('[data-slot="card-title"]', { hasText: /^Environment$/ })
  await expect(envTitle).toBeVisible()
  await expect(page.getByText(/Loading environment…/i)).toBeHidden()

  // Secret key initially masked
  const secretRow = page.locator('[data-env-key="SECRET_KEY"]').first()
  const secretReveal = secretRow.getByRole('button', { name: /Reveal secret/i })
  await expect(secretReveal).toBeVisible()
  await secretReveal.click()

  await expect(secretRow.getByLabel('Environment value')).toHaveValue(SECRET_VALUE)

  const portInput = page.locator('input[value="3000"]')
  await portInput.fill('4000')

  const secret = await getUserMfaSecret('admin@example.com')
  if (!secret) {
    throw new Error('admin@example.com missing TOTP secret')
  }

  const waitForSave = page.waitForResponse(
    (response) =>
      response.request().method() === 'PUT' &&
      response.url().includes('/api/v1/apps/rack-gateway/env') &&
      response.status() === 200
  )
  await clearStepUpSessions()
  await page.getByRole('button', { name: /Save Changes/i }).click()
  await satisfyMFAStepUpModal(page, { email: 'admin@example.com', secret, require: true })
  await waitForSave

  await expect(page.getByText(/Environment updated/i).first()).toBeVisible()
})
