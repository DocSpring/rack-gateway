import type { Locator, Page } from '@playwright/test'
import { authenticator } from 'otplib'
import { WebRoute } from '@/lib/routes'
import { expect, test } from './fixtures'
import { clearStepUpSessions, enforceMfaFor, login, resetMfaFor } from './helpers'

const ADMIN_EMAIL = 'admin@example.com'

function cardByTitle(page: Page, title: string): Locator {
  return page.locator('[data-slot="card"]').filter({
    has: page.locator('[data-slot="card-title"]', { hasText: title }),
  })
}

async function requireStepUp(page: Page) {
  await clearStepUpSessions()
  const statusResponsePromise = page.waitForResponse(
    (response) =>
      response.url().includes('/auth/mfa/status') && response.request().method() === 'GET'
  )
  await page.reload()
  await expect(page.getByRole('heading', { name: 'Account Security' })).toBeVisible()
  const statusResponse = await statusResponsePromise
  const statusData = (await statusResponse.json()) as { recent_step_up_expires_at?: string | null }
  if (statusData.recent_step_up_expires_at) {
    throw new Error('expected step-up window to reset before proceeding with sensitive action')
  }
  const loadingIndicator = page.getByText('Loading latest security information…', { exact: true })
  await expect(loadingIndicator).toHaveCount(0)
  await expect(page.getByRole('button', { name: /^Disable MFA$/ })).toBeEnabled()
}

async function completeStepUp(page: Page, secret: string) {
  const dialog = page.getByRole('dialog', { name: /Multi-Factor Authentication Required/i })
  await expect(dialog).toBeVisible()

  const code = authenticator.generate(secret)
  await dialog.getByLabel('Verification code').fill(code)
  await dialog.getByRole('button', { name: /^Verify$/ }).click()
  await expect(dialog).toBeHidden({ timeout: 4000 })
}

async function performLoginWithMfa(page: Page, secret: string, trustDevice: boolean) {
  await page.goto(WebRoute('login'))
  const btn = page
    .getByTestId('login-cta')
    .or(page.getByRole('button', { name: /Continue with/i }))
    .or(page.getByRole('link', { name: /Continue with/i }))
  await expect(btn).toBeVisible({ timeout: 5000 })
  const navPromise = page.waitForURL(/oauth2\/v2\/auth|dev\/select-user/i)
  await btn.click()
  await navPromise

  const userCard = page.locator('text=Admin User').first()
  await expect(userCard).toBeVisible()
  await userCard.click()

  const mfaDialog = page.getByRole('dialog', { name: /Multi-Factor Authentication Required/i })
  const isVisible = await mfaDialog.isVisible({ timeout: 5000 }).catch(() => false)
  if (!isVisible) {
    await page.waitForURL(/\.gateway\/web(?:\/|$)/, { timeout: 15_000 })
    return
  }

  const trustCheckbox = mfaDialog.getByLabel('Trust this browser for 30 days')
  const currentlyChecked = await trustCheckbox.isChecked().catch(() => false)
  if (trustDevice && !currentlyChecked) {
    await trustCheckbox.check()
  } else if (!trustDevice && currentlyChecked) {
    await trustCheckbox.uncheck()
  }

  await mfaDialog.getByLabel('Verification code').fill(authenticator.generate(secret))
  await mfaDialog.getByRole('button', { name: /^Verify$/ }).click()
  await expect(mfaDialog).toBeHidden({ timeout: 5000 })
  await page.waitForURL(/\.gateway\/web(?:\/|$)/, { timeout: 15_000 })
}

test.describe('Account security', () => {
  test.describe.configure({ mode: 'serial' })

  test.beforeEach(async () => {
    await resetMfaFor(ADMIN_EMAIL)
  })

  test('user can manage MFA enrollment, backup codes, trusted devices, and removal flows', async ({
    page,
  }) => {
    await login(page, { autoEnrollMfa: false })

    await page.goto(WebRoute('account/security'))
    await expect(page.getByRole('heading', { name: 'Account Security' })).toBeVisible()
    const mfaCard = cardByTitle(page, 'Multi-Factor Authentication').first()
    await expect(mfaCard).toBeVisible()
    await expect(mfaCard.getByText('Enabled', { exact: true })).toHaveCount(0)
    await expect(mfaCard.getByText('Disabled', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: /^Enable MFA$/ })).toBeEnabled()
    await expect(page.getByRole('button', { name: /^Disable MFA$/ })).toHaveCount(0)
    await expect(cardByTitle(page, 'Registered MFA Methods')).toHaveCount(0)
    await expect(cardByTitle(page, 'Backup Codes')).toHaveCount(0)

    // Click Enable MFA button
    await page.getByRole('button', { name: /^Enable MFA$/ }).click()

    // Wait for method selector to appear and choose TOTP
    await expect(cardByTitle(page, 'Choose MFA Method')).toBeVisible()

    const enrollmentResponsePromise = page.waitForResponse(
      (response) =>
        response.url().includes('/auth/mfa/enroll/totp/start') &&
        response.request().method() === 'POST'
    )
    await page.getByRole('button', { name: /TOTP Authenticator/i }).click()
    const enrollmentResponse = await enrollmentResponsePromise
    const enrollment = (await enrollmentResponse.json()) as { secret: string }

    await expect(page.getByText(/Finish MFA Enrollment/i)).toBeVisible()

    const secret = enrollment.secret
    await page.getByLabel(/Enter the 6-digit code to confirm/i).fill(authenticator.generate(secret))
    await page.getByRole('button', { name: /^Confirm$/ }).click()

    await expect(page.getByText(/Finish MFA Enrollment/i)).toHaveCount(0)

    await expect(mfaCard.getByText('Enabled', { exact: true })).toBeVisible()

    let methodsCard: Locator = cardByTitle(page, 'Registered MFA Methods').first()
    await expect(methodsCard).toBeVisible()
    const methodsTable = methodsCard.locator('table').first()
    await expect(methodsTable.locator('tbody tr')).toHaveCount(1)
    const methodRow = methodsTable.locator('tbody tr').first()
    await expect(methodRow.getByText('TOTP')).toBeVisible()

    const backupCard = cardByTitle(page, 'Backup Codes').first()
    await expect(backupCard.getByText('Unused codes', { exact: false })).toBeVisible()

    const trustedDevicesCard = cardByTitle(page, 'Trusted Devices').first()
    await expect(trustedDevicesCard.getByRole('button', { name: /^Revoke$/ })).toBeVisible()

    await requireStepUp(page)
    const regenResponsePromise = page.waitForResponse(
      (response) =>
        response.url().includes('/auth/mfa/backup-codes/regenerate') &&
        response.request().method() === 'POST'
    )
    await page.getByRole('button', { name: /^Regenerate backup codes$/ }).click()
    await completeStepUp(page, secret)
    await regenResponsePromise
    await expect(backupCard.getByRole('button', { name: /Download latest codes/i })).toBeVisible()

    await requireStepUp(page)
    const revokeResponsePromise = page.waitForResponse(
      (response) =>
        response.url().includes('/auth/mfa/trusted-devices/') &&
        response.request().method() === 'DELETE'
    )
    const revokeButton = trustedDevicesCard.getByRole('button', { name: /^Revoke$/ }).first()
    await revokeButton.click()
    await completeStepUp(page, secret)
    await revokeResponsePromise
    await expect(trustedDevicesCard.locator('tbody tr')).toHaveCount(0)

    await requireStepUp(page)
    const deleteResponsePromise = page.waitForResponse(
      (response) =>
        response.url().includes('/auth/mfa/methods/') && response.request().method() === 'DELETE'
    )
    await page.getByRole('button', { name: /^Disable MFA$/ }).click()
    const disableDialog = page.getByRole('dialog', { name: 'Disable MFA' })
    await expect(disableDialog).toBeVisible()
    await disableDialog.getByLabel('Confirmation').fill('DISABLE')
    await disableDialog.getByRole('button', { name: 'Disable MFA' }).click()
    await completeStepUp(page, secret)
    await deleteResponsePromise
    await expect(page.getByText('Disabled', { exact: true })).toBeVisible()
    await expect(cardByTitle(page, 'Registered MFA Methods')).toHaveCount(0)
    await expect(cardByTitle(page, 'Backup Codes')).toHaveCount(0)

    await page.getByRole('button', { name: /^Enable MFA$/ }).click()

    // Wait for method selector and choose TOTP
    await expect(cardByTitle(page, 'Choose MFA Method')).toBeVisible()

    const reEnrollResponsePromise = page.waitForResponse(
      (response) =>
        response.url().includes('/auth/mfa/enroll/totp/start') &&
        response.request().method() === 'POST'
    )
    await page.getByRole('button', { name: /TOTP Authenticator/i }).click()
    const reEnrollResponse = await reEnrollResponsePromise
    const reEnroll = (await reEnrollResponse.json()) as { secret: string }

    await page
      .getByLabel(/Enter the 6-digit code to confirm/i)
      .fill(authenticator.generate(reEnroll.secret))
    await page.getByRole('button', { name: /^Confirm$/ }).click()
    await expect(page.getByText(/Finish MFA Enrollment/i)).toHaveCount(0)

    await requireStepUp(page)
    const removeResponsePromise = page.waitForResponse(
      (response) =>
        response.url().includes('/auth/mfa/methods/') && response.request().method() === 'DELETE'
    )
    methodsCard = cardByTitle(page, 'Registered MFA Methods').first()
    await expect(methodsCard).toBeVisible()
    const removeButton = methodsCard.getByRole('button', { name: /^Remove$/ }).first()
    await removeButton.click()
    await completeStepUp(page, reEnroll.secret)
    await removeResponsePromise
    await expect(mfaCard.getByText('Disabled', { exact: true })).toBeVisible()
    await expect(cardByTitle(page, 'Registered MFA Methods')).toHaveCount(0)
  })

  test('user can edit MFA method labels', async ({ page }) => {
    await login(page, { autoEnrollMfa: false })

    await page.goto(WebRoute('account/security'))
    await expect(page.getByRole('heading', { name: 'Account Security' })).toBeVisible()

    // Enable MFA
    await page.getByRole('button', { name: /^Enable MFA$/ }).click()
    await expect(cardByTitle(page, 'Choose MFA Method')).toBeVisible()

    const enrollmentResponsePromise = page.waitForResponse(
      (response) =>
        response.url().includes('/auth/mfa/enroll/totp/start') &&
        response.request().method() === 'POST'
    )
    await page.getByRole('button', { name: /TOTP Authenticator/i }).click()
    const enrollmentResponse = await enrollmentResponsePromise
    const enrollment = (await enrollmentResponse.json()) as { secret: string }
    const secret = enrollment.secret

    await expect(page.getByText(/Finish MFA Enrollment/i)).toBeVisible()
    await page.getByLabel(/Enter the 6-digit code to confirm/i).fill(authenticator.generate(secret))
    await page.getByRole('button', { name: /^Confirm$/ }).click()
    await expect(page.getByText(/Finish MFA Enrollment/i)).toHaveCount(0)

    // Verify method was added
    const methodsCard = cardByTitle(page, 'Registered MFA Methods').first()
    await expect(methodsCard).toBeVisible()
    const methodsTable = methodsCard.locator('table').first()
    await expect(methodsTable.locator('tbody tr')).toHaveCount(1)

    // Find and click the edit button (pencil icon)
    const editButton = methodsTable.locator('tbody tr').first().getByRole('button').first()
    await editButton.click()

    // Verify edit dialog appears
    const editDialog = page.getByRole('dialog', { name: /Edit MFA Method Label/i })
    await expect(editDialog).toBeVisible()
    await expect(editDialog.getByLabel('Label')).toHaveValue('Authenticator App')

    // Change the label
    await editDialog.getByLabel('Label').fill('My Personal Authenticator')

    // Save the changes
    const updateResponsePromise = page.waitForResponse(
      (response) =>
        response.url().includes('/auth/mfa/methods/') && response.request().method() === 'PUT'
    )
    await editDialog.getByRole('button', { name: /^Save$/ }).click()
    await updateResponsePromise

    // Verify dialog closes
    await expect(editDialog).toBeHidden({ timeout: 4000 })

    // Verify the label was updated in the table
    await expect(
      methodsTable.locator('tbody tr').first().getByText('My Personal Authenticator')
    ).toBeVisible()
  })

  test('revoking trusted device forces MFA challenge on next login', async ({ page }) => {
    await resetMfaFor(ADMIN_EMAIL)

    await login(page, { autoEnrollMfa: false })

    await page.goto(WebRoute('account/security'))
    await expect(page.getByRole('heading', { name: 'Account Security' })).toBeVisible()

    const mfaCard = cardByTitle(page, 'Multi-Factor Authentication').first()
    await expect(mfaCard).toBeVisible()

    await page.getByRole('button', { name: /^Enable MFA$/ }).click()

    // Wait for method selector and choose TOTP
    await expect(cardByTitle(page, 'Choose MFA Method')).toBeVisible()

    const enrollmentResponsePromise = page.waitForResponse(
      (response) =>
        response.url().includes('/auth/mfa/enroll/totp/start') &&
        response.request().method() === 'POST'
    )
    await page.getByRole('button', { name: /TOTP Authenticator/i }).click()
    const enrollmentResponse = await enrollmentResponsePromise
    const enrollment = (await enrollmentResponse.json()) as { secret: string }
    const secret = enrollment.secret

    await expect(page.getByText(/Finish MFA Enrollment/i)).toBeVisible()
    await page.getByLabel(/Enter the 6-digit code to confirm/i).fill(authenticator.generate(secret))
    const trustCheckbox = page.getByLabel('Trust this browser for 30 days')
    if (!(await trustCheckbox.isChecked())) {
      await trustCheckbox.check()
    }
    await page.getByRole('button', { name: /^Confirm$/ }).click()
    await expect(page.getByText(/Finish MFA Enrollment/i)).toHaveCount(0)

    await enforceMfaFor(ADMIN_EMAIL)

    await page.getByRole('button', { name: /^Logout$/ }).click()
    await page.waitForURL(/\.gateway\/web\/login$/)
    await performLoginWithMfa(page, secret, true)

    await page.goto(WebRoute('account/security'))
    await expect(page.getByRole('heading', { name: 'Account Security' })).toBeVisible()

    const trustedDevicesCard = cardByTitle(page, 'Trusted Devices').first()
    await expect(trustedDevicesCard).toBeVisible()
    await expect(trustedDevicesCard.locator('tbody tr')).toHaveCount(1)

    await requireStepUp(page)

    const revokeResponsePromise = page.waitForResponse(
      (response) =>
        response.url().includes('/auth/mfa/trusted-devices/') &&
        response.request().method() === 'DELETE'
    )

    await trustedDevicesCard
      .getByRole('button', { name: /^Revoke$/ })
      .first()
      .click()

    const stepUpDialog = page.getByRole('dialog', {
      name: /Multi-Factor Authentication Required/i,
    })
    await expect(stepUpDialog.getByText('Multi-Factor Authentication Required')).toBeVisible({
      timeout: 15_000,
    })
    await stepUpDialog.getByLabel('Verification code').fill(authenticator.generate(secret))
    await stepUpDialog.getByRole('button', { name: /^Verify$/ }).click()
    await expect(stepUpDialog).toBeHidden({ timeout: 5000 })

    await revokeResponsePromise
    await expect(trustedDevicesCard.locator('tbody tr')).toHaveCount(0)

    await enforceMfaFor(ADMIN_EMAIL)

    await page.getByRole('button', { name: /^Logout$/ }).click()
    await page.waitForURL(/\.gateway\/web\/login$/)

    const btn = page
      .getByTestId('login-cta')
      .or(page.getByRole('button', { name: /Continue with/i }))
      .or(page.getByRole('link', { name: /Continue with/i }))
    await expect(btn).toBeVisible({ timeout: 5000 })
    const navPromise = page.waitForURL(/oauth2\/v2\/auth|dev\/select-user/i)
    await btn.click()
    await navPromise

    const userCard = page.locator('text=Admin User').first()
    await expect(userCard).toBeVisible()
    await userCard.click()

    await expect.poll(async () => page.url()).toMatch(/\.gateway\/web\/auth\/mfa\/challenge/i)

    await expect(page.getByText('Multi-Factor Authentication Required')).toBeVisible({
      timeout: 15_000,
    })
  })
})
