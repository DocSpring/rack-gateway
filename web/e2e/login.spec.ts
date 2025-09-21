import { expect, test } from './fixtures'

test('login button triggers gateway OAuth redirect', async ({ page }) => {
  page.on('pageerror', (e) => console.log('pageerror:', e))

  await page.goto('/.gateway/web/login')

  const btn = page
    .getByTestId('login-cta')
    .or(page.getByRole('button', { name: /Continue with/i }))
    .or(page.getByRole('link', { name: /Continue with/i }))

  try {
    await expect(btn).toBeVisible({ timeout: 5000 })
  } catch (e) {
    const html = await page.content()
    console.log('--- login page HTML (first 1200 chars) ---')
    console.log(html.slice(0, 1200))
    throw e
  }

  const navPromise = page.waitForURL(/oauth2\/v2\/auth|dev\/select-user/i)
  await btn.click()
  await navPromise
})
