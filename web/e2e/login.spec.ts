import { expect, test } from '@playwright/test'

test('login button triggers gateway OAuth redirect', async ({ page }) => {
  await page.goto('/login')

  const [resp] = await Promise.all([
    page.waitForResponse((r) => r.url().includes('/.gateway/web/login')),
    page.getByRole('button', { name: /Continue with (Mock OAuth|Google)/i }).click(),
  ])

  // Expect a redirect response from the gateway
  const status = resp.status()
  expect(status).toBeGreaterThanOrEqual(300)
  expect(status).toBeLessThan(400)

  const location = resp.headers()['location'] || resp.headers()['Location']
  expect(location).toBeTruthy()
  expect(location).toMatch(/oauth2\//i)
})
