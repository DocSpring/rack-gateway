import { WebRoute } from '@/lib/routes'
import { cleanupE2eArtifacts, seedAggregatedAuditLog } from './db'
import { expect, test } from './fixtures'
import { login } from './helpers'

test('aggregates repeated app list views into single audit entry', async ({ page }) => {
  await cleanupE2eArtifacts()
  await seedAggregatedAuditLog({
    action: 'app.list',
    actionType: 'convox',
    eventCount: 3,
    resource: 'all',
    resourceType: 'app',
    status: 'success',
    userEmail: 'admin@example.com',
    userName: 'Admin User',
  })

  await login(page)
  await page.goto(WebRoute('rack'))
  await expect(page.getByRole('heading', { name: 'Rack', exact: true })).toBeVisible()

  // Navigate to audit logs and confirm aggregation badge
  const auditLogsResponsePromise = page.waitForResponse(
    (response) =>
      response.url().includes('/api/v1/audit-logs') && response.request().method() === 'GET'
  )
  await page.goto(WebRoute('audit-logs'))
  const auditLogsResponse = await auditLogsResponsePromise
  expect(auditLogsResponse.ok()).toBeTruthy()
  await expect(page.getByRole('heading', { name: /Audit Logs/i })).toBeVisible()

  const table = page.getByRole('table')
  await expect(table).toBeVisible({ timeout: 15000 })

  // Scroll to table to ensure it's in screenshot
  await table.scrollIntoViewIfNeeded()

  const targetRow = table
    .getByRole('row', { name: /(app\.list|convox:app:list)/i })
    .filter({ hasText: /×\d+/i })
  await expect(targetRow).toHaveCount(1, { timeout: 15000 })

  const countBadge = targetRow.getByText(/×\d+/)
  const badgeText = (await countBadge.textContent())?.trim() ?? ''
  const count = Number.parseInt(badgeText.replace('×', ''), 10)
  expect(Number.isFinite(count) && count >= 3).toBeTruthy()
})
