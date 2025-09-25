import { WebRoute } from '@/lib/routes'
import { expect, test } from './fixtures'
import { login } from './helpers'

const SECRET_VALUE = 'super-secret-key-12345'

test('app environment management: reveal secret and save change', async ({ page }) => {
  await login(page)

  await page.goto(WebRoute('apps/convox-gateway/env'))
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

  await page.getByRole('button', { name: /Save Changes/i }).click()

  await expect(page.getByText(/Environment updated/i).first()).toBeVisible()
})
