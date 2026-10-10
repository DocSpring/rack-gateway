/**
 * A user who has lost their authenticator can sign in with one of the backup codes issued at
 * enrollment. Backup codes are 12 hex characters, typed into a separate form on the MFA challenge
 * page, and each one works once.
 */
import type { Page } from '@playwright/test'
import { authenticator } from 'otplib'
import { APIRoute, WebRoute } from '@/lib/routes'
import { clearMfaAttempts, getUserMfaSecret } from './db'
import { expect, test } from './fixtures'
import { clickLoginButton, isOnMfaChallengeUrl, login, resetMfaFor } from './helpers'

const ADMIN_EMAIL = 'admin@example.com'

async function regenerateBackupCodes(page: Page): Promise<string[]> {
  const secret = await getUserMfaSecret(ADMIN_EMAIL)
  if (!secret) {
    throw new Error(`No TOTP secret for ${ADMIN_EMAIL}`)
  }
  await clearMfaAttempts()
  const result = await page.evaluate(
    async ({ url, code }) => {
      const csrf = document.querySelector<HTMLMetaElement>('meta[name="rgw-csrf-token"]')?.content
      const response = await fetch(url, {
        method: 'POST',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrf ?? '',
          'X-MFA-TOTP': code,
        },
        body: '{}',
      })
      return { status: response.status, body: await response.json() }
    },
    { url: APIRoute('auth/mfa/backup-codes/regenerate'), code: authenticator.generate(secret) }
  )
  expect(result.status, JSON.stringify(result.body)).toBe(200)
  return (result.body as { backup_codes: string[] }).backup_codes
}

/** Signs in again from scratch and stops at the MFA challenge page. */
async function signInToMfaChallenge(page: Page) {
  await clearMfaAttempts()
  await page.context().clearCookies()
  await page.goto(WebRoute('login'))
  await clickLoginButton(page)
  const userCard = page.locator('text=Admin User').first()
  if (await userCard.isVisible().catch(() => false)) {
    await userCard.click()
  }
  await page.waitForURL((url) => isOnMfaChallengeUrl(url), { timeout: 15_000 })
}

async function submitBackupCode(page: Page, code: string) {
  await page.getByRole('button', { name: 'Use a backup code' }).click()
  await expect(page.getByLabel(/Trust this/i)).toHaveCount(0)
  await page.getByLabel('Backup code').fill(code)
  await page.getByRole('button', { name: 'Verify backup code' }).click()
}

test.describe('MFA backup codes', () => {
  test.beforeEach(async () => {
    await resetMfaFor(ADMIN_EMAIL)
  })

  test('signs in with a backup code once', async ({ page }) => {
    await login(page)
    const [code] = await regenerateBackupCodes(page)

    await signInToMfaChallenge(page)
    // Type it the way a person might: lowercase, with a dash between the groups
    await submitBackupCode(page, `${code.slice(0, 6).toLowerCase()}-${code.slice(6).toLowerCase()}`)
    await page.waitForURL((url) => !isOnMfaChallengeUrl(url), { timeout: 15_000 })
    await expect(page).toHaveURL(/\/app\/rack/)
    await expect(page.getByRole('dialog', { name: /Multi-Factor Authentication/i })).toHaveCount(0)

    await signInToMfaChallenge(page)
    await submitBackupCode(page, code)
    await expect(page.getByRole('alert').filter({ hasText: /verification failed/i })).toBeVisible()
    expect(isOnMfaChallengeUrl(new URL(page.url()))).toBe(true)
  })
})
