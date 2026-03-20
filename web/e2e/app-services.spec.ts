import { WebRoute } from '@/lib/routes'
import { getUserMfaSecret } from './db'
import { expect, test } from './fixtures'
import { clearStepUpSessions, login, satisfyMFAStepUpModal } from './helpers'

test('app services page can scale a service and stop a process', async ({ page }) => {
  await login(page)

  await page.goto(WebRoute('apps/rack-gateway/services'))
  const servicesTitle = page.locator('[data-slot="card-title"]', { hasText: /^Services$/ })
  await expect(servicesTitle).toBeVisible()

  const workerRow = page.getByTestId('service-row-worker-gj')
  const processCountCell = workerRow.locator('td').nth(1)
  const scaleCountValue = workerRow.locator('td').nth(2).locator('span').first()
  await expect(workerRow).toContainText('worker-gj')
  const initialProcessCount = Number((await processCountCell.textContent())?.trim())
  const initialScaleCount = Number((await scaleCountValue.textContent())?.trim())
  expect(initialProcessCount).toBeGreaterThanOrEqual(0)
  expect(initialScaleCount).toBeGreaterThanOrEqual(0)
  const targetScaleCount = Math.max(initialProcessCount, initialScaleCount) + 1

  const secret = await getUserMfaSecret('admin@example.com')
  if (!secret) {
    throw new Error('admin@example.com missing TOTP secret')
  }

  const waitForScale = page.waitForResponse(
    (response) =>
      response.request().method() === 'PUT' &&
      response.url().includes('/api/v1/convox/apps/rack-gateway/services/worker-gj') &&
      response.status() === 200
  )

  await clearStepUpSessions()
  await page.getByTestId('service-edit-worker-gj').click()
  await page.getByRole('spinbutton', { name: 'Scale for worker-gj' }).fill(String(targetScaleCount))
  await page.getByTestId('service-save-worker-gj').click()
  await satisfyMFAStepUpModal(page, { email: 'admin@example.com', secret, require: true })
  await waitForScale

  await expect(processCountCell).toHaveText(String(targetScaleCount))
  await expect(scaleCountValue).toHaveText(String(targetScaleCount))

  await page.getByTestId('app-tab-processes').click()
  const stopButton = page.locator('[data-testid^="stop-process-p-worker-gj-"]').first()
  await expect(stopButton).toBeVisible()
  const stopTestId = await stopButton.getAttribute('data-testid')
  if (!stopTestId) {
    throw new Error('worker-gj stop button is missing data-testid')
  }
  const processId = stopTestId.replace('stop-process-', '')

  const waitForStop = page.waitForResponse(
    (response) =>
      response.request().method() === 'DELETE' &&
      response.url().includes(`/api/v1/convox/apps/rack-gateway/processes/${processId}`) &&
      response.status() === 200
  )

  await clearStepUpSessions()
  await stopButton.click()
  await satisfyMFAStepUpModal(page, { email: 'admin@example.com', secret, require: true })
  await waitForStop

  await expect(page.getByTestId(stopTestId)).toHaveCount(0)

  await page.getByTestId('app-tab-services').click()
  await expect(processCountCell).toHaveText(String(targetScaleCount - 1))
  await expect(scaleCountValue).toHaveText(String(targetScaleCount))
})
