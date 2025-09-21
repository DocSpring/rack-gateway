import type { Page } from '@playwright/test'
import { expect } from './fixtures'

export interface LoginOptions {
  /**
   * Display text of the mock OAuth user card to select.
   * Defaults to "Admin User" when omitted.
   */
  userCardText?: string
}

export async function login(page: Page, options: LoginOptions = {}) {
  const { userCardText = 'Admin User' } = options

  await page.goto('/.gateway/web/login')
  const btn = page
    .getByTestId('login-cta')
    .or(page.getByRole('button', { name: /Continue with/i }))
    .or(page.getByRole('link', { name: /Continue with/i }))
  await expect(btn).toBeVisible({ timeout: 5000 })

  const navPromise = page.waitForURL(/oauth2\/v2\/auth|dev\/select-user/i)
  await btn.click()
  await navPromise

  const userCard = page.locator(`text=${userCardText}`)
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
