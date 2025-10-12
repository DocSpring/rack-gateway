import { expect, test } from './fixtures'
import { login } from './helpers'

test('app tabs show active styling per route', async ({ page }) => {
  await login(page)

  // Go to Apps list
  await page.goto('/app/apps')
  // Click first app link in table
  const firstAppLink = page.locator('table tbody tr a').first()
  await expect(firstAppLink).toBeVisible()
  await firstAppLink.click()

  // Processes tab should be active by default
  const tabProc = page.getByTestId('app-tab-processes')
  const tabBuilds = page.getByTestId('app-tab-builds')
  const tabReleases = page.getByTestId('app-tab-releases')
  await expect(tabProc).toHaveClass(/bg-accent/)
  await expect(tabProc).toHaveClass(/text-white/)
  await expect(tabBuilds).not.toHaveClass(/bg-primary/)
  await expect(tabReleases).not.toHaveClass(/bg-primary/)

  // Click Builds, verify active
  await tabBuilds.click()
  await expect(tabBuilds).toHaveClass(/bg-accent/)
  await expect(tabBuilds).toHaveClass(/text-white/)
  await expect(tabProc).not.toHaveClass(/bg-primary/)
  await expect(tabReleases).not.toHaveClass(/bg-primary/)

  // Click Releases, verify active
  await tabReleases.click()
  await expect(tabReleases).toHaveClass(/bg-accent/)
  await expect(tabReleases).toHaveClass(/text-white/)
  await expect(tabProc).not.toHaveClass(/bg-primary/)
  await expect(tabBuilds).not.toHaveClass(/bg-primary/)
})
