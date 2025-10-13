import type { Locator, Page } from '@playwright/test'
import { authenticator } from 'otplib'
import { WebRoute } from '@/lib/routes'
import { expect, test } from './fixtures'
import {
  clearStepUpSessions,
  enforceMfaFor,
  login,
  resetMfaFor,
  satisfyStepUpModal,
  setupBothMfaMethods,
  startTotpEnrollmentViaUi,
} from './helpers'

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
    await page.waitForURL(/app(?:\/|$)/, { timeout: 15_000 })
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
  // Auto-submits on 6-digit code, no need to click Verify button
  await expect(mfaDialog).toBeHidden({ timeout: 5000 })
  await page.waitForURL(/app(?:\/|$)/, { timeout: 15_000 })
}

test.describe('Account security', () => {
  test.describe.configure({ mode: 'serial' })

  test.beforeEach(async () => {
    await resetMfaFor(ADMIN_EMAIL)
  })

  test('user can manage MFA enrollment, backup codes, trusted devices, and removal flows', async ({
    page,
  }) => {
    page.on('request', (request) => {
      if (request.url().includes('mfa')) {
        console.log('[E2E request]', request.method(), request.url())
      }
    })

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

    const secret = await startTotpEnrollmentViaUi(page)
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
    await satisfyStepUpModal(page, { secret, require: true })
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
    await satisfyStepUpModal(page, { secret, require: true })
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
    await satisfyStepUpModal(page, { secret, require: true })
    await deleteResponsePromise
    await expect(page.getByText('Disabled', { exact: true })).toBeVisible()
    await expect(cardByTitle(page, 'Registered MFA Methods')).toHaveCount(0)
    await expect(cardByTitle(page, 'Backup Codes')).toHaveCount(0)

    await expect(page.getByRole('button', { name: /^Enable MFA$/ })).toBeEnabled()
    await expect(page.getByRole('button', { name: /^Disable MFA$/ })).toHaveCount(0)
    await expect(page.getByRole('button', { name: /^Enrollment In Progress$/ })).toHaveCount(0)

    // Enroll again with a new TOTP method
    const reEnrollSecret = await startTotpEnrollmentViaUi(page)

    const removeResponsePromise = page.waitForResponse(
      (response) =>
        response.url().includes('/auth/mfa/methods/') && response.request().method() === 'DELETE'
    )
    methodsCard = cardByTitle(page, 'Registered MFA Methods').first()
    await expect(methodsCard).toBeVisible()

    // Click the dropdown menu button and select "Remove Method"
    const dropdownButton = methodsCard.locator('tbody tr').first().getByRole('button')
    await dropdownButton.click()
    const removeMenuItem = page.getByText('Remove Method')
    await removeMenuItem.click()

    await satisfyStepUpModal(page, { secret: reEnrollSecret, require: true })

    await removeResponsePromise
    await expect(mfaCard.getByText('Disabled', { exact: true })).toBeVisible()
    await expect(cardByTitle(page, 'Registered MFA Methods')).toHaveCount(0)
  })

  test('user can edit MFA method labels', async ({ page }) => {
    await login(page, { autoEnrollMfa: false })

    await page.goto(WebRoute('account/security'))
    await expect(page.getByRole('heading', { name: 'Account Security' })).toBeVisible()

    await startTotpEnrollmentViaUi(page)

    // Verify method was added
    const methodsCard = cardByTitle(page, 'Registered MFA Methods').first()
    await expect(methodsCard).toBeVisible()
    const methodsTable = methodsCard.locator('table').first()
    await expect(methodsTable.locator('tbody tr')).toHaveCount(1)

    let editDialog = page.getByRole('dialog', { name: /Edit MFA Method Label/i })
    const dialogAutoVisible = await editDialog.isVisible().catch(() => false)

    if (!dialogAutoVisible) {
      const dropdownButton = methodsTable.locator('tbody tr').first().getByRole('button')
      await dropdownButton.click()
      const editMenuItem = page.getByText('Edit Label')
      await editMenuItem.click()
      editDialog = page.getByRole('dialog', { name: /Edit MFA Method Label/i })
    }

    await expect(editDialog).toBeVisible()
    await expect(editDialog.getByLabel('Label')).toHaveValue('Security Key')

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

    // Set up response listener BEFORE clicking Enable MFA
    const enrollmentResponsePromise = page.waitForResponse(
      (response) =>
        response.url().includes('/auth/mfa/enroll/totp/start') &&
        response.request().method() === 'POST'
    )

    // Click Enable MFA button - may show method selector or auto-start TOTP
    await page.getByRole('button', { name: /^Enable MFA$/ }).click()

    // Check if method selector appeared (WebAuthn available)
    const methodSelector = cardByTitle(page, 'Choose MFA Method')
    const methodSelectorVisible = await methodSelector.isVisible().catch(() => false)

    if (methodSelectorVisible) {
      // Method selector shown - click TOTP option
      await methodSelector.getByRole('button', { name: /TOTP Authenticator/ }).click()
    }

    // Wait for enrollment response (from either auto-start or after clicking TOTP)
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

    // Close the auto-opened edit modal (QOL feature opens edit dialog after enrollment)
    const editModal = page.getByText('Edit MFA Method Label')
    if (await editModal.isVisible().catch(() => false)) {
      await page.keyboard.press('Escape')
      await expect(editModal).toHaveCount(0)
    }

    await enforceMfaFor(ADMIN_EMAIL)

    await page.getByRole('button', { name: /^Logout$/ }).click()
    await page.waitForURL(/app\/login$/)
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
    // Auto-submits on 6-digit code, no need to click Verify button
    await expect(stepUpDialog).toBeHidden({ timeout: 5000 })

    await revokeResponsePromise
    await expect(trustedDevicesCard.locator('tbody tr')).toHaveCount(0)

    await enforceMfaFor(ADMIN_EMAIL)

    await page.getByRole('button', { name: /^Logout$/ }).click()
    await page.waitForURL(/app\/login$/)

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

    await expect.poll(async () => page.url()).toMatch(/web\/auth\/mfa\/challenge/i)

    await expect(page.getByText('Multi-Factor Authentication Required')).toBeVisible({
      timeout: 15_000,
    })
  })

  test('user can set and persist preferred MFA method', async ({ page }) => {
    // Set up user with both TOTP and WebAuthn methods via database
    // Note: This runs AFTER beforeEach which resets MFA
    await setupBothMfaMethods(ADMIN_EMAIL)
    await login(page, { autoEnrollMfa: false })

    await page.goto(WebRoute('account/security'))
    await expect(page.getByRole('heading', { name: 'Account Security' })).toBeVisible()

    // Wait for MFA status to load and verify both methods are shown
    const methodsCard = cardByTitle(page, 'Registered MFA Methods')
    await expect(methodsCard).toBeVisible({ timeout: 10_000 })

    // Verify we have the preferred method selector
    const preferredMethodSection = page.getByText('Preferred sign-in method')
    await expect(preferredMethodSection).toBeVisible()

    // Default should be TOTP (first in database)
    const totpRadio = page.getByRole('radio', { name: /TOTP Authenticator/i })
    const webauthnRadio = page.getByRole('radio', { name: /WebAuthn.*Security Key/i })

    await expect(totpRadio).toBeChecked()
    await expect(webauthnRadio).not.toBeChecked()

    // Switch to WebAuthn
    const updatePreferredPromise = page.waitForResponse(
      (response) =>
        response.url().includes('/auth/mfa/preferred-method') &&
        response.request().method() === 'PUT'
    )
    await webauthnRadio.click()
    await updatePreferredPromise

    // Verify selection changed
    await expect(webauthnRadio).toBeChecked()
    await expect(totpRadio).not.toBeChecked()

    // Reload the page and verify the preference persisted
    await page.reload()
    await expect(page.getByRole('heading', { name: 'Account Security' })).toBeVisible()

    // WebAuthn should still be selected after reload
    await expect(page.getByRole('radio', { name: /WebAuthn.*Security Key/i })).toBeChecked()
    await expect(page.getByRole('radio', { name: /TOTP Authenticator/i })).not.toBeChecked()

    // Switch back to TOTP
    const updateBackPromise = page.waitForResponse(
      (response) =>
        response.url().includes('/auth/mfa/preferred-method') &&
        response.request().method() === 'PUT'
    )
    await page.getByRole('radio', { name: /TOTP Authenticator/i }).click()
    await updateBackPromise

    // Verify it switched back
    await expect(page.getByRole('radio', { name: /TOTP Authenticator/i })).toBeChecked()
    await expect(page.getByRole('radio', { name: /WebAuthn.*Security Key/i })).not.toBeChecked()

    // Final reload to confirm persistence
    await page.reload()
    await expect(page.getByRole('heading', { name: 'Account Security' })).toBeVisible()
    await expect(page.getByRole('radio', { name: /TOTP Authenticator/i })).toBeChecked()
    await expect(page.getByRole('radio', { name: /WebAuthn.*Security Key/i })).not.toBeChecked()
  })

  test('login flow respects preferred MFA method', async ({ page }) => {
    // Set up user with both TOTP and WebAuthn, with WebAuthn as preferred
    await setupBothMfaMethods(ADMIN_EMAIL)
    await login(page, { autoEnrollMfa: false })

    await page.goto(WebRoute('account/security'))
    await expect(page.getByRole('heading', { name: 'Account Security' })).toBeVisible()

    // Set WebAuthn as preferred
    const webauthnRadio = page.getByRole('radio', { name: /WebAuthn.*Security Key/i })
    await expect(webauthnRadio).toBeVisible({ timeout: 10_000 })

    const updatePreferredPromise = page.waitForResponse(
      (response) =>
        response.url().includes('/auth/mfa/preferred-method') &&
        response.request().method() === 'PUT'
    )
    await webauthnRadio.click()
    await updatePreferredPromise

    // Enforce MFA and logout
    await enforceMfaFor(ADMIN_EMAIL)
    await page.getByRole('button', { name: /^Logout$/ }).click()
    await page.waitForURL(/app\/login$/)

    // Login and verify WebAuthn method is shown (not TOTP input)
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

    // WebAuthn starts automatically and succeeds (mocked in E2E)
    // Wait for navigation to complete, indicating successful WebAuthn verification
    await page.waitForURL(/\/web\/rack/, { timeout: 10_000 })
  })

  // NOTE: MFA rate limiting is tested in Go unit tests (TestVerifyTOTP_RateLimiting)
  // E2E test was removed because it's complex to set up with the serial test mode and beforeEach cleanup
})
