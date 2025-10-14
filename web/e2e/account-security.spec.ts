import type { Locator, Page } from '@playwright/test'
import { authenticator } from 'otplib'
import { WebRoute } from '@/lib/routes'
import { getUserMfaSecret } from './db'
import { expect, test } from './fixtures'
import {
  clearStepUpSessions,
  enforceMfaFor,
  login,
  resetMfaFor,
  satisfyMFAStepUpModal,
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

  // Wait for session cookie to be set
  await expect
    .poll(async () => {
      const cookies = await page.context().cookies()
      return cookies.some((cookie) => cookie.name === 'session_token')
    })
    .toBeTruthy()

  // Wait to see if we're redirected to MFA challenge page or directly to app
  try {
    await page.waitForURL(/auth\/mfa\/challenge/, { timeout: 3000 })
    // We're on the MFA challenge page - fill in the code
    const verificationInput = page.getByLabel('Verification code')
    await expect(verificationInput).toBeVisible({ timeout: 5000 })

    if (trustDevice) {
      const trustCheckbox = page.getByLabel(/Trust this/i)
      const checkboxExists = await trustCheckbox.isVisible().catch(() => false)
      if (checkboxExists) {
        const currentlyChecked = await trustCheckbox.isChecked().catch(() => false)
        if (!currentlyChecked) {
          await trustCheckbox.check()
        }
      }
    }

    await verificationInput.fill(authenticator.generate(secret))
    // Auto-submits on 6-digit code, wait for redirect
    await page.waitForURL(/app(?:\/|$)/, { timeout: 15_000 })
  } catch {
    // Not redirected to MFA challenge - might already be at /app or trusted device
    await page.waitForURL(/app(?:\/|$)/, { timeout: 15_000 })
  }
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
    const trustDeviceButton = trustedDevicesCard.getByRole('button', {
      name: /^Trust This Device$/,
    })
    const trustButtonCount = await trustDeviceButton.count()
    if (trustButtonCount > 0) {
      await trustDeviceButton.click()
      await satisfyMFAStepUpModal(page, { secret, trustDevice: true })
      await expect(trustedDevicesCard.locator('tbody tr')).toHaveCount(1)
    } else {
      await expect(trustedDevicesCard.locator('tbody tr')).not.toHaveCount(0)
    }

    await requireStepUp(page)
    const regenResponsePromise = page.waitForResponse(
      (response) =>
        response.url().includes('/auth/mfa/backup-codes/regenerate') &&
        response.request().method() === 'POST'
    )
    await page.getByRole('button', { name: /^Regenerate backup codes$/ }).click()
    await satisfyMFAStepUpModal(page, { secret, require: true })
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
    await satisfyMFAStepUpModal(page, { secret, require: true })
    await revokeResponsePromise
    await expect(trustedDevicesCard.locator('tbody tr')).toHaveCount(0, { timeout: 15_000 })

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
    await satisfyMFAStepUpModal(page, { secret, require: true })
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

    await satisfyMFAStepUpModal(page, { secret: reEnrollSecret, require: true })

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

    const secret = await startTotpEnrollmentViaUi(page)

    await enforceMfaFor(ADMIN_EMAIL)

    await page.getByRole('button', { name: /^Logout$/ }).click()
    await page.waitForURL(/app\/login$/)
    await performLoginWithMfa(page, secret, true)

    await page.goto(WebRoute('account/security'))
    await expect(page.getByRole('heading', { name: 'Account Security' })).toBeVisible()

    const trustedDevicesCard = cardByTitle(page, 'Trusted Devices').first()
    await expect(trustedDevicesCard).toBeVisible()
    const trustCurrentDeviceButton = trustedDevicesCard.getByRole('button', {
      name: /^Trust This Device$/,
    })
    if ((await trustCurrentDeviceButton.count()) > 0) {
      await trustCurrentDeviceButton.click()
      await satisfyMFAStepUpModal(page, { secret, trustDevice: true })
    }
    await expect(trustedDevicesCard.locator('tbody tr')).not.toHaveCount(0)

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

    await satisfyMFAStepUpModal(page, { secret, require: true })

    await revokeResponsePromise
    await expect(trustedDevicesCard.locator('tbody tr')).toHaveCount(0)

    await clearStepUpSessions()

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

    const verificationInput = page.getByLabel('Verification code')
    await expect(verificationInput).toBeVisible({ timeout: 15_000 })
    const trustCheckbox = page.getByLabel('Trust this device for 30 days')
    if ((await trustCheckbox.isVisible().catch(() => false)) && (await trustCheckbox.isChecked())) {
      await trustCheckbox.uncheck()
    }
    await verificationInput.fill(authenticator.generate(secret))
    await page.waitForURL(/app(?:\/|$)/, { timeout: 15_000 })
  })

  test('user can set and persist preferred MFA method', async ({ page }) => {
    // Use setupBothMfaMethods to create both TOTP and WebAuthn methods in DB
    // This must happen BEFORE any login so the session sees mfa_enrolled=true
    await setupBothMfaMethods(ADMIN_EMAIL)

    // Debug: Check if database was actually updated
    const { getUserMfaEnrolled } = await import('./db')
    await getUserMfaEnrolled(ADMIN_EMAIL)

    const secret = await getUserMfaSecret(ADMIN_EMAIL)
    if (!secret) {
      throw new Error('Expected TOTP secret after setupBothMfaMethods')
    }

    // Wait a bit for database to settle
    await new Promise((resolve) => setTimeout(resolve, 500))

    // Now login - user already has MFA enrolled in database
    // They'll be redirected to /auth/mfa/challenge after OAuth callback
    await performLoginWithMfa(page, secret, false)

    // Wait a bit for the session to be fully established after MFA verification
    await new Promise((resolve) => setTimeout(resolve, 1000))

    // Navigate to account security page
    await page.goto(WebRoute('account/security'))
    await expect(page.getByRole('heading', { name: 'Account Security' })).toBeVisible()

    // Wait for MFA status to load and verify both methods are shown
    const methodsCard = cardByTitle(page, 'Registered MFA Methods')
    await expect(methodsCard).toBeVisible({ timeout: 10_000 })

    // Verify we have the preferred method selector
    const preferredMethodSection = page.getByText('Preferred sign-in method')
    await expect(preferredMethodSection).toBeVisible()

    const totpRadio = page.getByRole('radio', { name: /TOTP Authenticator/i })
    const webauthnRadio = page.getByRole('radio', { name: /WebAuthn.*Security Key/i })

    // Check which method is currently selected
    const isTotpChecked = await totpRadio.isChecked()
    // Determine which method to switch to (the opposite of what's currently selected)
    const firstMethod = isTotpChecked ? 'WebAuthn / Security Key' : 'TOTP Authenticator'
    const secondMethod = isTotpChecked ? 'TOTP Authenticator' : 'WebAuthn / Security Key'
    const firstRadio = isTotpChecked ? webauthnRadio : totpRadio
    const secondRadio = isTotpChecked ? totpRadio : webauthnRadio

    // Expire the step-up session from login so we can test step-up modal
    await requireStepUp(page)

    // Switch to the other method - this will trigger step-up interceptor
    await page.getByLabel(firstMethod).click()

    // Satisfy step-up modal and wait for the API call to complete
    const satisfied = await satisfyMFAStepUpModal(page, { secret, require: true })
    if (!satisfied) {
      throw new Error('Expected step-up modal to appear when changing preferred MFA method')
    }

    await expect(firstRadio).toBeChecked({ timeout: 15_000 })

    // Verify selection changed
    await expect(firstRadio).toBeChecked()
    await expect(secondRadio).not.toBeChecked()

    // Reload the page and verify the preference persisted
    await page.reload()
    await expect(page.getByRole('heading', { name: 'Account Security' })).toBeVisible()

    // First method should still be selected after reload
    await expect(firstRadio).toBeChecked()
    await expect(secondRadio).not.toBeChecked()

    // Need to clear step-up session before switching again
    await requireStepUp(page)

    // Switch back to the second method - this will also trigger step-up
    await page.getByLabel(secondMethod).click()

    // Satisfy step-up modal again
    const satisfiedAgain = await satisfyMFAStepUpModal(page, { secret, require: true })
    if (!satisfiedAgain) {
      throw new Error('Expected step-up modal to appear when changing preferred MFA method back')
    }

    await expect(secondRadio).toBeChecked({ timeout: 15_000 })

    // Verify it switched back
    await expect(secondRadio).toBeChecked()
    await expect(firstRadio).not.toBeChecked()

    // Final reload to confirm persistence
    await page.reload()
    await expect(page.getByRole('heading', { name: 'Account Security' })).toBeVisible()
    await expect(secondRadio).toBeChecked()
    await expect(firstRadio).not.toBeChecked()
  })

  test('login flow respects preferred MFA method', async ({ page }) => {
    // Set up user with both TOTP and WebAuthn BEFORE login
    await setupBothMfaMethods(ADMIN_EMAIL)
    const secret = await getUserMfaSecret(ADMIN_EMAIL)
    if (!secret) {
      throw new Error('Expected TOTP secret for admin@example.com after setupBothMfaMethods')
    }

    // Wait a bit for database to settle
    await new Promise((resolve) => setTimeout(resolve, 500))

    // Login with MFA already enrolled - must use performLoginWithMfa since user has MFA
    await performLoginWithMfa(page, secret, false)

    await page.goto(WebRoute('account/security'))
    await expect(page.getByRole('heading', { name: 'Account Security' })).toBeVisible()

    // Set WebAuthn as preferred
    await expect(page.getByLabel('WebAuthn / Security Key')).toBeVisible({ timeout: 10_000 })

    // Expire step-up session from login so we can test step-up modal
    await clearStepUpSessions()
    await page.reload()
    await expect(page.getByRole('heading', { name: 'Account Security' })).toBeVisible()

    // Click to switch to WebAuthn - this will trigger step-up
    await page.getByLabel('WebAuthn / Security Key').click()

    // Satisfy step-up modal
    const satisfied = await satisfyMFAStepUpModal(page, { secret, require: true })
    if (!satisfied) {
      throw new Error('Expected step-up modal when setting preferred MFA method')
    }

    await expect(page.getByLabel('WebAuthn / Security Key')).toBeChecked({ timeout: 15_000 })

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
    await page.waitForURL(/\/app\/rack/, { timeout: 10_000 })
  })

  // NOTE: MFA rate limiting is tested in Go unit tests (TestVerifyTOTP_RateLimiting)
  // E2E test was removed because it's complex to set up with the serial test mode and beforeEach cleanup
})
