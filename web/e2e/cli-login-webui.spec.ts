/**
 * Test: CLI login flow followed by WebUI access
 *
 * This test verifies that users who authenticate via CLI can use the WebUI:
 * 1. User logs in via CLI (`rack-gateway login`)
 * 2. User completes OAuth and MFA in the browser
 * 3. User clicks "Open Web UI" button on the success page
 * 4. User can perform authenticated actions in the WebUI
 *
 * NOTE: Deploy approval "approve" action requires MFAAlways (inline MFA with each request).
 * The WebUI handles this by showing an MFA dialog and sending X-MFA-TOTP header.
 * This test simulates that by sending the TOTP code with the approve request.
 *
 * See: https://github.com/DocSpring/rack-gateway/issues/12
 */
import { authenticator } from 'otplib'
import { APIRoute, WebRoute } from '@/lib/routes'
import { createPendingDeployApprovalRequest, deleteDeployApprovalRequest } from './db'
import { expect, test } from './fixtures'
import { ensureMfaEnrollment, resetMfaFor } from './helpers'

const ADMIN_EMAIL = 'admin@example.com'

test.describe('CLI login to WebUI flow', () => {
  test.beforeEach(async () => {
    await resetMfaFor(ADMIN_EMAIL)
  })

  test('user authenticated via CLI login can perform actions in WebUI', async ({
    page,
    request,
  }) => {
    // Step 1: Start CLI login flow
    const startResponse = await request.post(APIRoute('auth/cli/start'))
    expect(startResponse.ok()).toBeTruthy()
    const startData = await startResponse.json()
    expect(startData.auth_url).toBeTruthy()
    expect(startData.state).toBeTruthy()

    // Step 2: Navigate to the OAuth URL (mock OAuth will redirect to callback)
    await page.goto(startData.auth_url)

    // Select the admin user card in mock OAuth
    const userCard = page.locator('text=Admin User').first()
    await expect(userCard).toBeVisible({ timeout: 5000 })
    await userCard.click()

    // Step 3: Since user doesn't have MFA enrolled, they're redirected directly to enrollment
    // The CLI state is preserved in the URL
    await page.waitForURL(/\/app\/account\/security.*enrollment=required/, { timeout: 10_000 })
    expect(page.url()).toContain('channel=cli')

    // Complete MFA enrollment
    const secret = await ensureMfaEnrollment(page, { email: ADMIN_EMAIL, useUi: true })

    // After enrollment, should redirect back to CLI success page
    await expect(page).toHaveURL(/\/app\/cli\/auth\/success/, { timeout: 15_000 })

    // Verify we're on the CLI success page
    await expect(page.getByText(/Authentication Complete/i)).toBeVisible()
    await expect(page.getByText(/Your CLI login is approved/i)).toBeVisible()

    // Step 5: Click "Open Web UI" button
    const openWebUIButton = page.getByRole('link', { name: /Open Web UI/i })
    await expect(openWebUIButton).toBeVisible()
    await openWebUIButton.click()

    // Should navigate to /app/ (or /app/rack as the default landing page)
    await page.waitForURL(/\/app\/(rack)?$/, { timeout: 10_000 })

    // Step 6: Verify we're authenticated by checking the info endpoint
    const infoResponse = await page.evaluate(async (endpoint) => {
      const response = await fetch(endpoint, { credentials: 'include' })
      let data: unknown = null
      try {
        data = await response.json()
      } catch {
        // Ignore JSON parse errors
      }
      return { ok: response.ok, status: response.status, data }
    }, APIRoute('info'))

    // Get CSRF token from the page (needed for POST requests)
    const csrfToken = await page.evaluate(() => {
      const meta = document.querySelector<HTMLMetaElement>('meta[name="rgw-csrf-token"]')
      return meta?.content ?? null
    })
    expect(csrfToken).toBeTruthy()

    expect(infoResponse.ok).toBeTruthy()
    expect(infoResponse.status).toBe(200)
    expect((infoResponse.data as { user?: { email?: string } })?.user?.email).toBe(ADMIN_EMAIL)

    // Step 6b: Check MFA status to understand session state
    const mfaStatusResponse = await page.evaluate(async (endpoint) => {
      const response = await fetch(endpoint, { credentials: 'include' })
      return response.json()
    }, APIRoute('auth/mfa/status'))

    // Verify step-up is valid (MFA was verified during enrollment)
    expect(mfaStatusResponse.recent_step_up_expires_at).toBeTruthy()

    // Step 7: Create a deploy approval request to test approving it
    const approvalPublicId = await createPendingDeployApprovalRequest()

    try {
      // Step 8: Test the approve API with inline MFA (MFAAlways requirement)
      // The approve endpoint requires inline MFA with every request.
      // The WebUI's MFA dialog handles this by prompting for TOTP and sending X-MFA-TOTP header.
      // Here we simulate that by generating a TOTP code and sending it with the request.
      const totpCode = authenticator.generate(secret)

      const approveResponse = await page.evaluate(
        async ({ endpoint, publicId, code, csrf }) => {
          const response = await fetch(`${endpoint}/${publicId}/approve`, {
            method: 'POST',
            credentials: 'include',
            headers: {
              'Content-Type': 'application/json',
              'X-MFA-TOTP': code,
              'X-CSRF-Token': csrf,
            },
            body: JSON.stringify({}),
          })
          let body: unknown = null
          try {
            body = await response.json()
          } catch {
            // Ignore parse errors
          }
          return { ok: response.ok, status: response.status, body }
        },
        {
          endpoint: APIRoute('deploy-approval-requests'),
          publicId: approvalPublicId,
          code: totpCode,
          csrf: csrfToken as string,
        }
      )

      // The approve request should succeed when MFA code is provided
      expect(
        approveResponse.ok,
        `POST to approve endpoint failed with status ${approveResponse.status}. ` +
          `Body: ${JSON.stringify(approveResponse.body)}. ` +
          'CLI login sessions should properly authenticate POST requests with MFA.'
      ).toBeTruthy()

      // Navigate to deploy approvals to verify the status changed
      await page.goto(WebRoute('deploy-approval-requests'))
      await expect(page.getByRole('heading', { name: /Deploy Approvals/i })).toBeVisible({
        timeout: 10_000,
      })

      // Verify the approval was successful by checking the status changed
      // Look for a table cell containing the "approved" status text
      await expect(page.locator('table td:has-text("approved")').first()).toBeVisible({
        timeout: 5000,
      })
    } finally {
      // Cleanup
      await deleteDeployApprovalRequest(approvalPublicId)
    }
  })

  test('API calls after CLI login work correctly', async ({ page, request }) => {
    // This is a more focused test to verify session cookies are working

    // Step 1: Start CLI login flow
    const startResponse = await request.post(APIRoute('auth/cli/start'))
    expect(startResponse.ok()).toBeTruthy()
    const startData = await startResponse.json()

    // Step 2: Navigate to the OAuth URL
    await page.goto(startData.auth_url)

    // Select the admin user card
    const userCard = page.locator('text=Admin User').first()
    await expect(userCard).toBeVisible({ timeout: 5000 })
    await userCard.click()

    // Step 3: Since user doesn't have MFA enrolled, they're redirected directly to enrollment
    await page.waitForURL(/\/app\/account\/security.*enrollment=required/, { timeout: 10_000 })

    // Complete MFA enrollment
    await ensureMfaEnrollment(page, { email: ADMIN_EMAIL, useUi: true })

    // After enrollment, should be on CLI success page
    await expect(page).toHaveURL(/\/app\/cli\/auth\/success/, { timeout: 15_000 })

    // Step 5: Verify session cookie was set
    const cookies = await page.context().cookies()
    const sessionCookie = cookies.find((c) => c.name === 'session_token')
    expect(sessionCookie).toBeTruthy()

    // Navigate to the app to get CSRF token (we're on CLI success page, need to go to app)
    await page.goto(WebRoute('rack'))
    await page.waitForURL(/\/app\/rack/, { timeout: 10_000 })

    // Get CSRF token from the page (needed for POST requests)
    const csrfToken = await page.evaluate(() => {
      const meta = document.querySelector<HTMLMetaElement>('meta[name="rgw-csrf-token"]')
      return meta?.content ?? null
    })
    expect(csrfToken).toBeTruthy()

    // Step 6: Test GET API calls work with the session
    // Use page.evaluate to make fetch calls with credentials
    const getTestCases = [
      { endpoint: 'info', method: 'GET' },
      { endpoint: 'auth/mfa/status', method: 'GET' },
      { endpoint: 'deploy-approval-requests', method: 'GET' },
    ]

    for (const { endpoint, method } of getTestCases) {
      const result = await page.evaluate(
        async ({ url, httpMethod }) => {
          const response = await fetch(url, {
            method: httpMethod,
            credentials: 'include',
            headers: { 'Content-Type': 'application/json' },
          })
          return { ok: response.ok, status: response.status }
        },
        { url: APIRoute(endpoint), httpMethod: method }
      )

      expect(
        result.ok,
        `Expected ${endpoint} to return ok, got status ${result.status}`
      ).toBeTruthy()
    }

    // Step 7: Test POST API call works (MFANone route - creating deploy approval requests)
    // This verifies that POST requests work after CLI login for routes that don't require MFAAlways
    const postResult = await page.evaluate(
      async ({ url, csrf }) => {
        const response = await fetch(url, {
          method: 'POST',
          credentials: 'include',
          headers: {
            'Content-Type': 'application/json',
            'X-CSRF-Token': csrf,
          },
          body: JSON.stringify({
            app: 'test-app',
            message: 'E2E test deploy approval request',
            git_commit_hash: 'abc123def456',
            git_branch: 'test-branch',
          }),
        })
        let body: unknown = null
        try {
          body = await response.json()
        } catch {
          // Ignore parse errors
        }
        return { ok: response.ok, status: response.status, body }
      },
      { url: APIRoute('deploy-approval-requests'), csrf: csrfToken as string }
    )

    // The request should be authenticated (not 401/403)
    // A 400 validation error is acceptable because it means auth worked but the payload was invalid
    // We're specifically testing that the session cookie and CSRF token work for POST requests
    expect(
      postResult.status !== 401 && postResult.status !== 403,
      `POST deploy-approval-requests failed with auth error status ${postResult.status}. ` +
        `Body: ${JSON.stringify(postResult.body)}. ` +
        'This indicates CLI login sessions are not properly authenticating POST requests.'
    ).toBeTruthy()
  })
})
