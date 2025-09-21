import type { Page } from '@playwright/test'
import { expect, test } from './fixtures'

async function login(page: Page) {
  await page.goto('/.gateway/web/login')
  const btn = page
    .getByTestId('login-cta')
    .or(page.getByRole('button', { name: /Continue with/i }))
    .or(page.getByRole('link', { name: /Continue with/i }))
  await expect(btn).toBeVisible({ timeout: 5000 })
  const navPromise = page.waitForURL(/oauth2\/v2\/auth|dev\/select-user/i)
  await btn.click()
  await navPromise

  const userCard = page.locator('text=Admin User')
  if (
    await userCard
      .first()
      .isVisible()
      .catch(() => false)
  ) {
    await userCard.first().click()
  }

  await page.waitForResponse((r) => r.url().includes('/.gateway/api/me') && r.status() === 200, {
    timeout: 20_000,
  })
}

test('app tabs show active styling per route', async ({ page }) => {
  await login(page)

  // Go to Apps list
  await page.goto('/.gateway/web/apps')
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
