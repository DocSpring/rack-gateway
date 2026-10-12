import { WebRoute } from '@/lib/routes'
import { cleanupE2eArtifacts, ensureAdminUser, seedAggregatedAuditLog } from './db'
import { expect, test } from './fixtures'
import { login } from './helpers'

test.describe('Audit aggregation', () => {
  test('aggregates repeated app list views into single audit entry', async ({ page }) => {
    test.setTimeout(60_000)
    await cleanupE2eArtifacts()
    await ensureAdminUser()
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

    // Navigate to the Convox app events in the audit log and confirm the aggregation badge. Every
    // request is recorded, so logging in and loading the Rack page alone fill the first page of the
    // unfiltered log.
    const auditLogsResponsePromise = page.waitForResponse(
      (response) =>
        new URL(response.url()).pathname === '/api/v1/audit-logs' &&
        response.request().method() === 'GET'
    )
    await page.goto(`${WebRoute('audit-logs')}?action_type=convox&resource_type=app`)
    const auditLogsResponse = await auditLogsResponsePromise
    expect(auditLogsResponse.ok()).toBeTruthy()
    await expect(page.getByRole('heading', { name: /Audit Logs/i })).toBeVisible()

    const table = page.getByRole('table')
    await expect(table).toBeVisible({ timeout: 15_000 })

    // Scroll to table to ensure it's in screenshot
    await table.scrollIntoViewIfNeeded()

    const targetRow = table
      .getByRole('row', { name: /(app\.list|convox:app:list)/i })
      .filter({ hasText: /×\d+/i })
    await expect(targetRow).toHaveCount(1, { timeout: 15_000 })

    const countBadge = targetRow.getByText(/×\d+/)
    const badgeText = (await countBadge.textContent())?.trim() ?? ''
    const count = Number.parseInt(badgeText.replace('×', ''), 10)
    expect(Number.isFinite(count) && count >= 3).toBeTruthy()
  })
})
