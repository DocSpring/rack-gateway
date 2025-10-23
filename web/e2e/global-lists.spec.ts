import { expect, test } from './fixtures'
import { login } from './helpers'

test('visit global Processes page', async ({ page }) => {
  await login(page)
  await page.goto('/app/processes')
  await expect(page.getByRole('heading', { name: /Processes/i })).toBeVisible()
  // Either table or empty/loading state should render without JS errors
  const table = page.getByRole('table')
  const loading = page.getByText(/Loading processes/i)
  const spinner = page.locator('[data-slot="card"] .animate-spin')
  const empty = page.getByText(/No processes found/i)
  await expect(table.or(loading).or(spinner).or(empty)).toBeVisible({ timeout: 5000 })
  await expect(table.or(empty)).toBeVisible({ timeout: 15_000 })
})

test('visit global Instances page', async ({ page }) => {
  await login(page)
  await page.goto('/app/instances')
  await expect(page.getByRole('heading', { name: /Instances/i })).toBeVisible()
  const table = page.getByRole('table')
  const loading = page.getByText(/Loading instances/i)
  const spinner = page.locator('[data-slot="card"] .animate-spin')
  const empty = page.getByText(/No instances found/i)
  await expect(table.or(loading).or(spinner).or(empty)).toBeVisible({ timeout: 5000 })
  await expect(table.or(empty)).toBeVisible({ timeout: 15_000 })
})

test('visit global Builds page', async ({ page }) => {
  await login(page)
  await page.goto('/app/builds')
  await expect(page.getByRole('heading', { name: /Builds/i })).toBeVisible()
  const table = page.getByRole('table')
  const loading = page.getByText(/Loading builds/i)
  const spinner = page.locator('[data-slot="card"] .animate-spin')
  const empty = page.getByText(/No builds found/i)
  await expect(table.or(loading).or(spinner).or(empty)).toBeVisible({ timeout: 5000 })
  await expect(table.or(empty)).toBeVisible({ timeout: 15_000 })
})

test('visit global Releases page', async ({ page }) => {
  await login(page)
  await page.goto('/app/releases')
  await expect(page.getByRole('heading', { name: /Releases/i })).toBeVisible()
  const table = page.getByRole('table')
  const loading = page.getByText(/Loading releases/i)
  const spinner = page.locator('[data-slot="card"] .animate-spin')
  const empty = page.getByText(/No releases found/i)
  await expect(table.or(loading).or(spinner).or(empty)).toBeVisible({ timeout: 5000 })
  await expect(table.or(empty)).toBeVisible({ timeout: 15_000 })
})
