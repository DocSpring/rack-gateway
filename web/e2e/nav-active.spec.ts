import { WebRoute } from '@/lib/routes'
import { expect, test } from './fixtures'
import { login } from './helpers'

test('sidebar nav link shows active styling', async ({ page }) => {
  await login(page)

  await page.goto(WebRoute('rack'))
  await page.getByRole('link', { name: 'Builds' }).waitFor({ state: 'visible' })
  await page.getByRole('link', { name: 'Builds' }).click()
  const builds = page.getByRole('link', { name: 'Builds' })
  await expect(builds).toHaveClass(/bg-accent/)
  await expect(builds).toHaveClass(/text-foreground/)
})
