import type { Page } from '@playwright/test'
import { authenticator } from 'otplib'
import { WebRoute } from '@/lib/routes'
import {
  clearMfaAttempts,
  enforceMfaForUser,
  expireStepUpForAllSessions,
  getPendingTotpSecret,
  getUserMfaSecret,
  resetMfaForUser,
  setupBothMfaMethodsForUser,
} from './db'
import { expect } from './fixtures'

type E2EWindow = Window &
  typeof globalThis & {
    __e2e_totpSecret?: string | null
    __e2e_clipboardStubbed?: boolean
  }

type LoginOptions = {
  /**
   * Display text of the mock OAuth user card to select.
   * Defaults to "Admin User" when omitted.
   */
  userCardText?: string
  /**
   * Email address for the mock OAuth user card. Defaults to the email matching the card text.
   */
  email?: string
  /**
   * When true (default), ensure the authenticated user has completed MFA enrollment
   * so gated routes remain accessible during tests. Disable for scenarios that
   * explicitly exercise the enrollment UX.
   */
  autoEnrollMfa?: boolean
}

const CARD_TEXT_TO_EMAIL: Record<string, string> = {
  'Admin User': 'admin@example.com',
  'Deployer User': 'deployer@example.com',
  'Viewer User': 'viewer@example.com',
  'Ops User': 'ops@example.com',
}

export async function login(page: Page, options: LoginOptions = {}) {
  const { userCardText = 'Admin User', autoEnrollMfa = true } = options
  const email = options.email ?? CARD_TEXT_TO_EMAIL[userCardText] ?? 'admin@example.com'

  // console.log(`[LOGIN] Starting login for ${email}, page.context().baseURL = ${(page.context() as any)._options?.baseURL}`)

  // Clear MFA rate limit attempts before each login to prevent test pollution
  await clearMfaAttempts()

  await page.goto(WebRoute('login'))
  const btn = page
    .getByTestId('login-cta')
    .or(page.getByRole('button', { name: /Continue with/i }))
    .or(page.getByRole('link', { name: /Continue with/i }))
  await expect(btn).toBeVisible({ timeout: 5000 })

  const navPromise = page.waitForURL(/oauth2\/v2\/auth|dev\/select-user/i)
  await btn.click()
  await navPromise

  const userCard = page.locator(`text=${userCardText}`)
  if (
    await userCard
      .first()
      .isVisible()
      .catch(() => false)
  ) {
    await userCard.first().click()
  }

  await expect
    .poll(async () => {
      const cookies = await page.context().cookies()
      return cookies.some((cookie) => cookie.name === 'session_token')
    })
    .toBeTruthy()

  await page.waitForURL(/app(?:\/|$)/, { timeout: 15_000 })

  if (autoEnrollMfa) {
    await ensureMfaEnrollment(page, { email })
  }
}

export async function resetMfaFor(email: string) {
  await resetMfaForUser(email)
}

export async function enforceMfaFor(email: string) {
  await enforceMfaForUser(email)
}

export async function clearStepUpSessions() {
  await expireStepUpForAllSessions()
  await clearMfaAttempts()
}

/**
 * Types an OTP code digit by digit into the verification code input.
 * This properly triggers the auto-advance logic in the OTPInput component.
 * Looks for the input within the provided context (page or dialog).
 */
export async function typeOtpCode(
  page: Page,
  context: Page | ReturnType<Page['getByRole']>,
  code: string
) {
  const fieldset = context.locator('[aria-label="Verification code"]')
  const inputs = fieldset.locator('input')
  const firstInput = inputs.first()
  await firstInput.click()
  const inputCount = await inputs.count()
  for (let index = 0; index < inputCount - 1; index += 1) {
    await page.keyboard.press('ArrowRight')
  }
  for (let index = 0; index < inputCount; index += 1) {
    await page.keyboard.press('Backspace')
  }
  for (const digit of code) {
    await page.keyboard.type(digit)
  }
}

export async function setupBothMfaMethods(email: string) {
  await setupBothMfaMethodsForUser(email)
}

export async function ensureMfaEnrollment(
  page: Page,
  options: { email?: string } = {}
): Promise<string> {
  const email = options.email ?? 'admin@example.com'
  const existing = await getUserMfaSecret(email)
  if (existing) {
    return existing
  }

  await page.goto(WebRoute('account/security'))
  await expect(page.getByRole('heading', { name: 'Account Security' })).toBeVisible()

  const secret = await startTotpEnrollmentViaUi(page, email)

  await expect(page.getByRole('heading', { name: 'Account Security' })).toBeVisible()
  const statusBadge = page.locator('[data-slot="card"]').first().getByText('Enabled')
  await expect(statusBadge).toBeVisible()

  return secret
}

export async function satisfyMFAStepUpModal(
  page: Page,
  options: {
    email?: string
    secret?: string
    trustDevice?: boolean
    clearMfaAttempts?: boolean
    require?: boolean
  } = {}
): Promise<boolean> {
  const {
    email = 'admin@example.com',
    secret: secretOverride,
    trustDevice = false,
    require: requireDialog = true,
  } = options
  const shouldClearMfaAttempts = options.clearMfaAttempts ?? true

  if (shouldClearMfaAttempts) {
    await clearMfaAttempts()
  }

  const dialog = page.getByRole('dialog', { name: /Multi-Factor Authentication Required/i })
  await dialog.waitFor({ state: 'visible', timeout: requireDialog ? 5000 : 2000 })

  const secret = secretOverride ?? (await getUserMfaSecret(email))
  if (!secret) {
    throw new Error(`No confirmed TOTP secret found for ${email}. Ensure ensureMfaEnrollment ran.`)
  }

  // Check if the modal is in WebAuthn mode (showing "Authenticate with Security Key" button)
  // If so, switch to TOTP mode by clicking "Use authenticator app instead"
  const webAuthnButton = dialog.getByRole('button', { name: /Authenticate with Security Key/i })
  const isWebAuthnMode = await webAuthnButton.isVisible().catch(() => false)

  if (isWebAuthnMode) {
    const switchToTotpButton = dialog.getByRole('button', {
      name: /Use authenticator app instead/i,
    })
    await switchToTotpButton.click()
    await expect(dialog.getByLabel(/Verification code/i)).toBeVisible({ timeout: 2000 })
  }

  // Wait for the input field to be ready (after switching if needed)
  const input = dialog.getByLabel(/Verification code/i)
  await expect(input).toBeVisible({ timeout: 2000 })

  const trustCheckbox = dialog.getByLabel(/Trust this/i)
  const checkboxExists = await trustCheckbox.isVisible().catch(() => false)
  if (checkboxExists) {
    const checked = await trustCheckbox.isChecked().catch(() => false)
    if (trustDevice && !checked) {
      await trustCheckbox.check()
    } else if (!trustDevice && checked) {
      await trustCheckbox.uncheck()
    }
  }

  const code = authenticator.generate(secret)
  await typeOtpCode(page, dialog, code)

  // Wait for the dialog to close after auto-submit
  await expect(dialog).toBeHidden({ timeout: 5000 })
  return true
}

export async function startTotpEnrollmentViaUi(
  page: Page,
  email = 'admin@example.com'
): Promise<string> {
  // console.log(`[START_TOTP_ENROLLMENT] Starting for ${email}`)
  await page.evaluate(() => {
    const globalWindow = window as unknown as E2EWindow
    globalWindow.__e2e_totpSecret = null
    if (globalWindow.__e2e_clipboardStubbed) return

    const stub = (text: string) => {
      const win = window as unknown as E2EWindow
      win.__e2e_totpSecret = text
      return Promise.resolve()
    }

    navigator.clipboard.writeText = stub as typeof navigator.clipboard.writeText
    globalWindow.__e2e_clipboardStubbed = true
  })

  await page.getByRole('button', { name: /^Enable MFA$/ }).click()

  await page.getByRole('button', { name: 'Authenticator app', exact: true }).click()

  const enrollmentDialog = page.getByRole('dialog', {
    name: 'Enable Multi-Factor Authentication',
  })
  await expect(enrollmentDialog).toBeVisible()

  const copySecretButton = enrollmentDialog.getByRole('button', {
    name: 'Copy secret for manual entry',
  })
  await expect(copySecretButton).toBeVisible()
  await copySecretButton.click()

  await expect
    .poll(async () =>
      page.evaluate(() => {
        const win = window as unknown as E2EWindow
        return win.__e2e_totpSecret ?? ''
      })
    )
    .not.toEqual('')

  let secret = (await page.evaluate(() => {
    const win = window as unknown as E2EWindow
    return win.__e2e_totpSecret ?? null
  })) as string | null

  if (!secret) {
    secret = await pollPendingTotpSecret(email, 5000)
  }

  if (!secret) {
    throw new Error(`Failed to retrieve pending TOTP secret during enrollment for ${email}`)
  }

  const code = authenticator.generate(secret)
  await typeOtpCode(page, enrollmentDialog, code)

  // Click the Confirm button (enrollment doesn't have auto-submit)
  await enrollmentDialog.getByRole('button', { name: /^Confirm$/ }).click()

  await expect(enrollmentDialog).toBeHidden()

  const editLabelDialog = page
    .getByRole('dialog', { name: 'Edit MFA Method Label' })
    .filter({ has: page.getByRole('heading', { name: 'Edit MFA Method Label' }) })
  const labelDialogVisible = await editLabelDialog.isVisible({ timeout: 1000 }).catch(() => false)
  if (labelDialogVisible) {
    await editLabelDialog.getByRole('button', { name: /^Save$/ }).click()
    await expect(editLabelDialog).toBeHidden()
  }

  return secret
}

async function pollPendingTotpSecret(email: string, timeoutMs = 5000): Promise<string | null> {
  const start = Date.now()
  while (Date.now() - start < timeoutMs) {
    const secret = await getPendingTotpSecret(email)
    if (secret) {
      return secret
    }
    await new Promise((resolve) => setTimeout(resolve, 150))
  }
  return null
}
