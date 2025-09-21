import { expect, test } from './fixtures'
import { login } from './helpers'

test('visit global Processes page', async ({ page }) => {
  await login(page)
  await page.goto('/.gateway/web/processes')
  await expect(page.getByRole('heading', { name: /Processes/i })).toBeVisible()
  // Either table or empty/loading state should render without JS errors
  const table = page.getByRole('table')
  const loading = page.getByText(/Loading processes/i)
  await expect(table.or(loading)).toBeVisible()
})

test('visit global Instances page', async ({ page }) => {
  await login(page)
  await page.goto('/.gateway/web/instances')
  await expect(page.getByRole('heading', { name: /Instances/i })).toBeVisible()
  const table = page.getByRole('table')
  const loading = page.getByText(/Loading instances/i)
  await expect(table.or(loading)).toBeVisible()
})

test('visit global Builds page', async ({ page }) => {
  await login(page)
  await page.goto('/.gateway/web/builds')
  await expect(page.getByRole('heading', { name: /Builds/i })).toBeVisible()
  const table = page.getByRole('table')
  const loading = page.getByText(/Loading builds/i)
  await expect(table.or(loading)).toBeVisible()
})

test('visit global Releases page', async ({ page }) => {
  await login(page)
  await page.goto('/.gateway/web/releases')
  await expect(page.getByRole('heading', { name: /Releases/i })).toBeVisible()
  const table = page.getByRole('table')
  const loading = page.getByText(/Loading releases/i)
  await expect(table.or(loading)).toBeVisible()
})
