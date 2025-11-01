import { expect, test } from './fixtures'
import { login } from './helpers'

test('visit Background Jobs page', async ({ page }) => {
  await login(page)
  await page.goto('/app/jobs')
  await expect(page.getByRole('heading', { name: /Background Jobs/i })).toBeVisible()

  // Either table or empty/loading state should render without JS errors
  const table = page.getByRole('table')
  const loading = page.locator('[data-slot="card"] .animate-spin')
  const empty = page.getByText(/No background jobs found/i)
  await expect(table.or(loading).or(empty)).toBeVisible({ timeout: 5000 })
  await expect(table.or(empty)).toBeVisible({ timeout: 15_000 })
})

test('action buttons appear in job row', async ({ page }) => {
  await login(page)
  await page.goto('/app/jobs')

  // Check that the page loads
  await expect(page.getByRole('heading', { name: /Background Jobs/i })).toBeVisible()

  // If there are jobs in the table, verify action buttons exist
  const table = page.getByRole('table')
  const empty = page.getByText(/No background jobs found/i)

  // Either see the table or the empty message
  await expect(table.or(empty)).toBeVisible({ timeout: 15_000 })

  // If table has rows, verify eye (view), retry (RotateCw), and X (delete) buttons exist
  const hasRows = await table.locator('tbody tr').count()
  if (hasRows > 0) {
    const firstRow = table.locator('tbody tr').first()
    const actionsCell = firstRow.locator('td').last()

    // Should have eye, retry, and delete buttons (3 buttons total)
    await expect(actionsCell.getByRole('button').first()).toBeVisible()
    await expect(actionsCell.getByRole('button').nth(1)).toBeVisible()
    await expect(actionsCell.getByRole('button').nth(2)).toBeVisible()
  }
})
