import type { Page } from '@playwright/test'
import { authenticator } from 'otplib'
import { APIRoute, WebRoute } from '@/lib/routes'
import {
  clearMfaAttempts,
  enforceMfaForUser,
  expireStepUpForAllSessions,
  resetMfaForUser,
  setupBothMfaMethodsForUser,
} from './db'
import { expect } from './fixtures'

export type LoginOptions = {
  /**
   * Display text of the mock OAuth user card to select.
   * Defaults to "Admin User" when omitted.
   */
  userCardText?: string
  /**
   * When true (default), ensure the authenticated user has completed MFA enrollment
   * so gated routes remain accessible during tests. Disable for scenarios that
   * explicitly exercise the enrollment UX.
   */
  autoEnrollMfa?: boolean
}

export async function login(page: Page, options: LoginOptions = {}) {
  const { userCardText = 'Admin User', autoEnrollMfa = true } = options

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

  await page.waitForURL(/\.gateway\/web(?:\/|$)/, { timeout: 15_000 })

  if (autoEnrollMfa) {
    await ensureMfaEnrollment(page)
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

export async function setupBothMfaMethods(email: string) {
  await setupBothMfaMethodsForUser(email)
}

export async function ensureMfaEnrollment(page: Page) {
  await page
    .waitForFunction(
      () => {
        const meta = document.querySelector<HTMLMetaElement>('meta[name="cgw-csrf-token"]')
        const value = meta?.content?.trim()
        return Boolean(value && value !== 'CGW_CSRF_TOKEN')
      },
      undefined,
      { timeout: 5000 }
    )
    .catch(() => {})

  const csrfToken = await page.evaluate(() => {
    const meta = document.querySelector<HTMLMetaElement>('meta[name="cgw-csrf-token"]')
    const value = meta?.content?.trim()
    if (!value || value === 'CGW_CSRF_TOKEN') {
      return ''
    }
    return value
  })
  const headers: Record<string, string> = {
    Accept: 'application/json',
    'Content-Type': 'application/json',
  }
  if (csrfToken) {
    headers['X-CSRF-Token'] = csrfToken
  }

  const startResp = await page.request.post(APIRoute('auth/mfa/enroll/totp/start'), {
    headers,
  })
  if (startResp.status() >= 400) {
    const detail = await startResp.text().catch(() => '')
    if (startResp.status() === 409) {
      return
    }
    throw new Error(
      `failed to start MFA enrollment (${startResp.status()}): ${detail || 'no response body'}`
    )
  }

  const startData = (await startResp.json()) as { method_id: number; secret: string }
  if (!(startData?.method_id && startData?.secret)) {
    return
  }

  const code = authenticator.generate(startData.secret)
  const confirmResp = await page.request.post(APIRoute('auth/mfa/enroll/totp/confirm'), {
    data: {
      method_id: startData.method_id,
      code,
      trust_device: true,
    },
    headers,
  })

  if (confirmResp.status() >= 400) {
    const detail = await confirmResp.text().catch(() => '')
    throw new Error(
      `failed to confirm MFA enrollment (${confirmResp.status()}): ${detail || 'no response body'}`
    )
  }

  // Wait a bit for any navigation to settle before reloading
  await page.waitForLoadState('domcontentloaded').catch(() => {})
  await page.waitForTimeout(500)

  try {
    await page.reload({ waitUntil: 'networkidle' })
  } catch (err) {
    // If reload fails due to navigation, wait for it to complete
    if (err instanceof Error && err.message.includes('ERR_ABORTED')) {
      await page.waitForLoadState('networkidle')
    } else {
      throw err
    }
  }

  await expect
    .poll(async () => {
      const statusResp = await page.request.get(APIRoute('auth/mfa/status'))
      if (statusResp.status() >= 400) {
        return false
      }
      const payload = (await statusResp.json()) as { enrolled?: boolean }
      return payload?.enrolled === true
    })
    .toBe(true)
}
