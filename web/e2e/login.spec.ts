import { test } from './fixtures'
import { clickLoginButton } from './helpers'

test('login button triggers gateway OAuth redirect', async ({ page }) => {
  await page.goto('/app/login')
  await clickLoginButton(page)
})
