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

test('sidebar nav link shows active styling', async ({ page }) => {
  await login(page)
  await page.goto('/.gateway/web/builds')
  const builds = page.getByRole('link', { name: 'Builds' })
  await expect(builds).toHaveClass(/bg-accent/)
  await expect(builds).toHaveClass(/text-white/)
})
