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
  await expect(workerRow).toContainText('worker-gj')
  await expect(workerRow).toContainText('1')

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
  await page.getByRole('spinbutton', { name: 'Scale for worker-gj' }).fill('3')
  await page.getByTestId('service-save-worker-gj').click()
  await satisfyMFAStepUpModal(page, { email: 'admin@example.com', secret, require: true })
  await waitForScale

  await expect(workerRow).toContainText('worker-gj')
  await expect(workerRow).toContainText('3')

  await page.getByTestId('app-tab-processes').click()
  const stopButton = page.getByTestId('stop-process-p-worker-gj-1')
  await expect(stopButton).toBeVisible()

  const waitForStop = page.waitForResponse(
    (response) =>
      response.request().method() === 'DELETE' &&
      response.url().includes('/api/v1/convox/apps/rack-gateway/processes/p-worker-gj-1') &&
      response.status() === 200
  )

  await clearStepUpSessions()
  await stopButton.click()
  await satisfyMFAStepUpModal(page, { email: 'admin@example.com', secret, require: true })
  await waitForStop

  await expect(page.getByTestId('stop-process-p-worker-gj-1')).toHaveCount(0)

  await page.getByTestId('app-tab-services').click()
  await expect(workerRow).toContainText('worker-gj')
  await expect(workerRow).toContainText('2')
  await expect(workerRow).toContainText('3')
})
