import { expect, test } from '@playwright/test'

test('login button redirects to mock OAuth selection', async ({ page }) => {
  await page.goto('/login')
  await page.getByRole('button', { name: /Continue with (Mock OAuth|Google)/i }).click()

  // The mock OAuth server redirects to a user selection page when no user is chosen
  await page.waitForURL(/\/dev\/select-user|oauth2\/v2\/auth/i, { timeout: 10000 })

  const url = page.url()
  expect(url).toMatch(/dev\/select-user|oauth2\/v2\/auth/i)
})

