import { WebRoute } from '@/lib/routes'
import { expect, test } from './fixtures'
import { ensureMfaEnrollment, login } from './helpers'

test.describe('Configure CLI dialog', () => {
  test('shows rack alias in login instructions', async ({ page }) => {
    await login(page)
    await ensureMfaEnrollment(page)
    await page.goto(WebRoute('rack'))

    await page.getByRole('button', { name: /Configure CLI/i }).click()

    const dialog = page.getByRole('dialog', { name: 'Configure CLI' })
    await expect(dialog).toBeVisible()
    await expect(dialog).toContainText('rack-gateway login test http://localhost:')
  })
})
