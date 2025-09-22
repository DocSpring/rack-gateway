import { expect, test } from './fixtures'
import { login } from './helpers'

function extractAlias(label: string | null): string {
  if (!label) return 'default'
  const parts = label.split(':')
  if (parts.length < 2) return label.trim() || 'default'
  const alias = parts.slice(1).join(':').trim()
  return alias || 'default'
}

test.describe('Configure CLI dialog', () => {
  test('shows rack alias in login instructions', async ({ page }) => {
    await login(page)

    const rackText = await page.locator('text=Rack:').first().textContent()
    const rackAlias = extractAlias(rackText)

    await page.getByRole('button', { name: /Configure CLI/i }).click()

    const dialog = page.getByRole('dialog', { name: 'Configure CLI' })
    await expect(dialog).toBeVisible()
    await expect(dialog).toContainText(`convox-gateway login ${rackAlias}`)
  })
})
