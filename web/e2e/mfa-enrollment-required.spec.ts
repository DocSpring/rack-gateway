import type { Page } from '@playwright/test'
import { WebRoute } from '@/lib/routes'
import { expect, test } from './fixtures'
import { enforceMfaFor, login, resetMfaFor } from './helpers'

const ADMIN_EMAIL = 'admin@example.com'

async function expectRedirectToAccountSecurity(page: Page) {
  // Initial redirect may land on the MFA challenge page before bouncing to account/security.
  await page.waitForURL(
    (url) =>
      url.pathname.includes('/account/security') || url.pathname.includes('/auth/mfa/challenge'),
    { timeout: 10_000 }
  )

  if (page.url().includes('/auth/mfa/challenge')) {
    await page.waitForURL((url) => url.pathname.includes('/account/security'), {
      timeout: 10_000,
    })
  }

  await expect(page).toHaveURL(/account\/security/, { timeout: 10_000 })
}

test.describe('MFA enrollment enforcement', () => {
  test('admin user without MFA is redirected to account/security on login', async ({ page }) => {
    // Reset MFA for admin user to simulate first-time login
    await resetMfaFor(ADMIN_EMAIL)
    // Explicitly enforce MFA to ensure the requirement is active regardless of global settings
    await enforceMfaFor(ADMIN_EMAIL)

    // Login without auto-enrolling MFA
    await login(page, { autoEnrollMfa: false })

    // Should be redirected to account/security page after login (via challenge bounce)
    await expectRedirectToAccountSecurity(page)

    // Should show the security page heading
    await expect(page.getByRole('heading', { name: /Security/i })).toBeVisible()

    // Should show MFA enrollment section and the enable button
    await expect(page.getByRole('button', { name: 'Enable MFA' })).toBeVisible()
  })

  test('sidebar navigation links are disabled when MFA enrollment required', async ({ page }) => {
    // Reset MFA for admin user
    await resetMfaFor(ADMIN_EMAIL)
    await enforceMfaFor(ADMIN_EMAIL)

    // Login without auto-enrolling MFA
    await login(page, { autoEnrollMfa: false })

    await expectRedirectToAccountSecurity(page)

    // Check that sidebar links are disabled (rendered as non-links)
    // The "Rack" item should be rendered as a span, not a link, so getByRole('link') should not find it
    const rackLink = page.getByRole('link', { name: /^Rack$/i })
    await expect(rackLink).toHaveCount(0)

    // Verify the text is still visible (as a span) and appears disabled
    const rackText = page.locator('nav').getByText(/^Rack$/i)
    await expect(rackText).toBeVisible()
    // Check for the disabled styling class we use in Layout.tsx
    await expect(rackText).toHaveClass(/opacity-50/)
  })

  test('attempting to navigate to /rack redirects back to account/security', async ({ page }) => {
    // Reset MFA for admin user
    await resetMfaFor(ADMIN_EMAIL)
    await enforceMfaFor(ADMIN_EMAIL)

    // Login without auto-enrolling MFA
    await login(page, { autoEnrollMfa: false })

    await expectRedirectToAccountSecurity(page)

    // Try to navigate directly to /rack
    await page.goto(WebRoute('rack'))

    // Should be redirected back to account/security (with redirect parameter to preserve original destination)
    await expect(page).toHaveURL(/\/app\/account\/security/, { timeout: 10_000 })
  })

  test('attempting to navigate to /users redirects back to account/security', async ({ page }) => {
    // Reset MFA for admin user
    await resetMfaFor(ADMIN_EMAIL)
    await enforceMfaFor(ADMIN_EMAIL)

    // Login without auto-enrolling MFA
    await login(page, { autoEnrollMfa: false })

    await expectRedirectToAccountSecurity(page)

    // Try to navigate directly to /users
    await page.goto(WebRoute('users'))

    // Should be redirected back to account/security (with redirect parameter to preserve original destination)
    await expect(page).toHaveURL(/\/app\/account\/security/, { timeout: 10_000 })
  })

  test('after completing MFA enrollment, user can navigate freely', async ({ page }) => {
    test.setTimeout(60_000)
    // Reset MFA for admin user
    await resetMfaFor(ADMIN_EMAIL)
    await enforceMfaFor(ADMIN_EMAIL)

    // Login without auto-enrolling MFA
    await login(page, { autoEnrollMfa: false })

    await expectRedirectToAccountSecurity(page)

    // Now enroll in MFA using the helper
    const { ensureMfaEnrollment } = await import('./helpers')
    await ensureMfaEnrollment(page)

    // After enrollment, should be able to navigate to /rack
    await page.goto(WebRoute('rack'))
    await expect(page.getByRole('heading', { name: 'Rack', exact: true })).toBeVisible({
      timeout: 10_000,
    })

    // Should be able to navigate to other pages
    await page.goto(WebRoute('users'))
    await expect(page.getByRole('heading', { name: /Users/i })).toBeVisible()
  })
})
