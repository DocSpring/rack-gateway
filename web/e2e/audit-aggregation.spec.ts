import { expect, test } from './fixtures'
import { login } from './helpers'

test('aggregates repeated app list views into single audit entry', async ({ page }) => {
  await login(page)

  // Trigger three consecutive app list fetches directly to avoid interleaving logs
  for (let i = 0; i < 3; i += 1) {
    const response = await page.request.get('/api/v1/convox/apps')
    expect(response.ok()).toBeTruthy()
  }

  // Navigate to audit logs and confirm aggregation badge
  await page.goto('/app/audit_logs')
  await expect(page.getByRole('heading', { name: /Audit Logs/i })).toBeVisible()

  const table = page.getByRole('table')
  await expect(table).toBeVisible()

  // Scroll to table to ensure it's in screenshot
  await table.scrollIntoViewIfNeeded()

  const targetRow = table.getByRole('row', { name: /app\.list/i }).filter({ hasText: /×\d+/i })
  await expect(targetRow).toHaveCount(1)

  const countBadge = targetRow.getByText(/×\d+/)
  const badgeText = (await countBadge.textContent())?.trim() ?? ''
  const count = Number.parseInt(badgeText.replace('×', ''), 10)
  expect(Number.isFinite(count) && count >= 3).toBeTruthy()
})
