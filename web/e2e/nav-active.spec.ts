import { expect, test } from './fixtures'
import { login } from './helpers'

test('sidebar nav link shows active styling', async ({ page }) => {
  await login(page)
  await page.goto('/.gateway/web/builds')
  const builds = page.getByRole('link', { name: 'Builds' })
  await expect(builds).toHaveClass(/bg-accent/)
  await expect(builds).toHaveClass(/text-white/)
})
