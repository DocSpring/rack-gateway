import { expect, test } from '@playwright/test'

test('login button redirects to mock OAuth selection', async ({ page }) => {
  await page.goto('/login')
  await page.getByRole('button', { name: /Continue with (Mock OAuth|Google)/i }).click()

  // Wait for cross-origin navigation to OAuth server
  await page.waitForLoadState('load')

  const url = page.url()
  expect(url).toMatch(/dev\/select-user|oauth2\//i)
})
