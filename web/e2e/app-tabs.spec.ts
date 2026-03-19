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

  // Services tab should be active by default
  const tabServices = page.getByTestId('app-tab-services')
  const tabProc = page.getByTestId('app-tab-processes')
  const tabBuilds = page.getByTestId('app-tab-builds')
  const tabReleases = page.getByTestId('app-tab-releases')
  await expect(page).toHaveURL(/\/app\/apps\/[^/]+\/services$/)
  await expect(tabServices).toHaveClass(/bg-accent/)
  await expect(tabServices).toHaveClass(/text-white/)
  await expect(tabProc).not.toHaveClass(/text-white/)
  await expect(tabBuilds).not.toHaveClass(/text-white/)
  await expect(tabReleases).not.toHaveClass(/text-white/)

  // Click Processes, verify active
  await tabProc.click()
  await expect(tabProc).toHaveClass(/bg-accent/)
  await expect(tabProc).toHaveClass(/text-white/)
  await expect(tabServices).not.toHaveClass(/text-white/)
  await expect(tabBuilds).not.toHaveClass(/text-white/)
  await expect(tabReleases).not.toHaveClass(/text-white/)

  // Click Builds, verify active
  await tabBuilds.click()
  await expect(tabBuilds).toHaveClass(/bg-accent/)
  await expect(tabBuilds).toHaveClass(/text-white/)
  await expect(tabServices).not.toHaveClass(/text-white/)
  await expect(tabProc).not.toHaveClass(/text-white/)
  await expect(tabReleases).not.toHaveClass(/text-white/)

  // Click Releases, verify active
  await tabReleases.click()
  await expect(tabReleases).toHaveClass(/bg-accent/)
  await expect(tabReleases).toHaveClass(/text-white/)
  await expect(tabServices).not.toHaveClass(/text-white/)
  await expect(tabProc).not.toHaveClass(/text-white/)
  await expect(tabBuilds).not.toHaveClass(/text-white/)
})
