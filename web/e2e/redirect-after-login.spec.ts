import { WebRoute } from '@/lib/routes'
import { expect, test } from './fixtures'
import { clickLoginButton, resetMfaFor } from './helpers'

const ADMIN_EMAIL = 'admin@example.com'

test.describe('Redirect after login', () => {
  test.beforeEach(async () => {
    // Reset MFA for clean state - ensureMfaEnrollment in login() will set it up
    await resetMfaFor(ADMIN_EMAIL)
  })

  test('unauthenticated visit to protected page includes returnTo in login redirect', async ({
    page,
  }) => {
    // Navigate to a protected page without authentication
    const targetPath = '/app/deploy-approval-requests'
    await page.goto(targetPath)

    // Should redirect to login page with returnTo parameter
    await expect(page).toHaveURL(/\/app\/login\?returnTo=/, { timeout: 10_000 })

    const url = new URL(page.url())
    const returnTo = url.searchParams.get('returnTo')
    expect(returnTo).toBe(targetPath)
  })

  test('successful login redirects to original protected page after MFA enrollment', async ({
    page,
  }) => {
    // Navigate to a protected page without authentication
    const targetPath = '/app/deploy-approval-requests'
    await page.goto(targetPath)

    // Should redirect to login page with returnTo parameter
    await expect(page).toHaveURL(/\/app\/login\?returnTo=/, { timeout: 10_000 })

    // Complete the OAuth login flow (click button and select user)
    await clickLoginButton(page)

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

    // After OAuth completes, user will be redirected to MFA enrollment flow
    // because they don't have MFA enrolled yet and it's required
    await page.waitForURL(
      (url) =>
        url.pathname.includes('/account/security') || url.pathname.includes('/auth/mfa/challenge'),
      { timeout: 10_000 }
    )

    // If we land on challenge page first, wait for redirect to account/security
    if (page.url().includes('/auth/mfa/challenge')) {
      await page.waitForURL((url) => url.pathname.includes('/account/security'), {
        timeout: 10_000,
      })
    }

    // Complete MFA enrollment
    const { ensureMfaEnrollment } = await import('./helpers')
    await ensureMfaEnrollment(page)

    // After enrollment, should redirect back to the original target page
    await expect(page).toHaveURL(targetPath, { timeout: 10_000 })

    // Verify we're actually on the deploy approval requests page
    await expect(page.getByRole('heading', { name: /Deploy Approval Requests/i })).toBeVisible()
  })

  test('invalid returnTo (external URL) is ignored and redirects to default page', async ({
    page,
  }) => {
    // Try to login with an invalid returnTo (external URL)
    await page.goto(`${WebRoute('login')}?returnTo=https://evil.com/phishing`)

    // Complete the OAuth login flow
    await clickLoginButton(page)

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

    // Should be redirected to MFA enrollment (account/security) or default page
    // NOT to the evil URL
    await page.waitForURL(
      (url) =>
        url.pathname.includes('/app/rack') ||
        url.pathname.includes('/account/security') ||
        url.pathname.includes('/auth/mfa/challenge'),
      { timeout: 10_000 }
    )

    // Make sure we're NOT on the evil URL
    expect(page.url()).not.toContain('evil.com')
    expect(page.url()).not.toContain('phishing')
  })

  test('returnTo that is not an app path is ignored and redirects to default page', async ({
    page,
  }) => {
    // Try to login with a returnTo that doesn't start with /app/
    await page.goto(`${WebRoute('login')}?returnTo=/auth/callback`)

    // Complete the OAuth login flow
    await clickLoginButton(page)

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

    // Should be redirected to MFA enrollment or default page
    // NOT to /auth/callback
    await page.waitForURL(
      (url) =>
        url.pathname.includes('/app/rack') ||
        url.pathname.includes('/account/security') ||
        url.pathname.includes('/auth/mfa/challenge'),
      { timeout: 10_000 }
    )

    // Make sure we're NOT on /auth/callback
    expect(page.url()).not.toContain('/auth/callback')
  })

  test('returnTo is preserved through MFA enrollment flow', async ({ page }) => {
    // Start at a protected page (unauthenticated)
    const targetPath = '/app/users'
    await page.goto(targetPath)

    // Should redirect to login with returnTo
    await expect(page).toHaveURL(/\/app\/login\?returnTo=/, { timeout: 10_000 })
    expect(new URL(page.url()).searchParams.get('returnTo')).toBe(targetPath)

    // Complete OAuth flow
    await clickLoginButton(page)

    const userCard = page.locator('text=Admin User').first()
    await expect(userCard).toBeVisible()
    await userCard.click()

    // Wait for session cookie
    await expect
      .poll(async () => {
        const cookies = await page.context().cookies()
        return cookies.some((cookie) => cookie.name === 'session_token')
      })
      .toBeTruthy()

    // Will be redirected to MFA enrollment
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

    // Complete MFA enrollment
    const { ensureMfaEnrollment } = await import('./helpers')
    await ensureMfaEnrollment(page)

    // Should redirect to the original target path, not the default
    await expect(page).toHaveURL(targetPath, { timeout: 10_000 })
    await expect(page.getByRole('heading', { name: /Users/i })).toBeVisible()
  })

  test('returnTo works for already-logged-in user navigating to new protected page', async ({
    page,
  }) => {
    // First, complete a full login with MFA enrollment
    const { login } = await import('./helpers')
    await login(page)

    // Verify we're logged in by navigating to a page
    await page.goto(WebRoute('rack'))
    await expect(page.getByRole('heading', { name: 'Rack', exact: true })).toBeVisible()

    // Now simulate session expiration by clearing cookies
    await page.context().clearCookies()

    // Try to navigate to a different protected page
    const targetPath = '/app/api-tokens'
    await page.goto(targetPath)

    // Should redirect to login with returnTo
    await expect(page).toHaveURL(/\/app\/login\?returnTo=/, { timeout: 10_000 })
    expect(new URL(page.url()).searchParams.get('returnTo')).toBe(targetPath)

    // Complete OAuth flow (MFA already enrolled in database)
    await clickLoginButton(page)

    const userCard = page.locator('text=Admin User').first()
    await expect(userCard).toBeVisible()
    await userCard.click()

    // Wait for session cookie
    await expect
      .poll(async () => {
        const cookies = await page.context().cookies()
        return cookies.some((cookie) => cookie.name === 'session_token')
      })
      .toBeTruthy()

    // Should redirect directly to the target page (MFA already enrolled)
    await expect(page).toHaveURL(targetPath, { timeout: 10_000 })
    await expect(page.getByRole('heading', { name: /API Tokens/i })).toBeVisible()
  })
})
