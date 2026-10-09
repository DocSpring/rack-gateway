/**
 * Test: CLI login flow followed by WebUI access
 *
 * This test verifies the RFC 8252 loopback CLI login and that the browser used for it can use the WebUI:
 * 1. The CLI (played by the test) listens on 127.0.0.1 and starts a login with a PKCE challenge
 * 2. User completes OAuth and MFA enrollment in the browser
 * 3. The browser is handed back to the CLI's loopback listener with a single-use login code
 * 4. The CLI redeems the code with its PKCE verifier
 * 5. User can perform authenticated actions in the WebUI
 *
 * NOTE: Deploy approval "approve" action requires MFAAlways (inline MFA with each request).
 * The WebUI handles this by showing an MFA dialog and sending X-MFA-TOTP header.
 * This test simulates that by sending the TOTP code with the approve request.
 *
 * See: https://github.com/DocSpring/rack-gateway/issues/12
 */
import type { APIRequestContext, Page } from '@playwright/test'
import { authenticator } from 'otplib'
import { APIRoute, WebRoute } from '@/lib/routes'
import { type CliLoopbackLogin, startCliLoopbackLogin } from './cli-loopback'
import {
  clearMfaAttempts,
  createPendingDeployApprovalRequest,
  deleteDeployApprovalRequest,
} from './db'
import { expect, test } from './fixtures'
import { ensureMfaEnrollment, resetMfaFor } from './helpers'

const ADMIN_EMAIL = 'admin@example.com'
const LOOPBACK_URL = /^http:\/\/127\.0\.0\.1:\d+\/callback/

/** Runs the browser half of a CLI login for an unenrolled admin and returns the new TOTP secret. */
async function approveCliLoginWithEnrollment(page: Page, cli: CliLoopbackLogin): Promise<string> {
  await page.goto(cli.authUrl)

  const userCard = page.locator('text=Admin User').first()
  await expect(userCard).toBeVisible({ timeout: 5000 })
  await userCard.click()

  // The user has no MFA factor yet, so the browser goes to enrollment with the CLI login preserved
  await page.waitForURL(/\/app\/account\/security.*enrollment=required/, { timeout: 10_000 })
  expect(page.url()).toContain('channel=cli')

  // After enrolling, the browser is handed back to the CLI's loopback listener
  const secret = await ensureMfaEnrollment(page, { email: ADMIN_EMAIL, useUi: true })
  await expect(page).toHaveURL(LOOPBACK_URL, { timeout: 15_000 })
  await expect(page.getByText(/Login approved/i)).toBeVisible()
  return secret
}

async function withCliLogin(
  request: APIRequestContext,
  run: (cli: CliLoopbackLogin) => Promise<void>
): Promise<void> {
  const cli = await startCliLoopbackLogin(request)
  try {
    await run(cli)
  } finally {
    await cli.close()
  }
}

async function readCsrfToken(page: Page): Promise<string> {
  const csrfToken = await page.evaluate(() => {
    const meta = document.querySelector<HTMLMetaElement>('meta[name="rgw-csrf-token"]')
    return meta?.content ?? null
  })
  expect(csrfToken).toBeTruthy()
  return csrfToken as string
}

async function fetchJson(page: Page, endpoint: string) {
  return await page.evaluate(async (url) => {
    const response = await fetch(url, { credentials: 'include' })
    let data: unknown = null
    try {
      data = await response.json()
    } catch {
      // Ignore JSON parse errors
    }
    return { ok: response.ok, status: response.status, data }
  }, endpoint)
}

test.describe('CLI login to WebUI flow', () => {
  test.beforeEach(async () => {
    await resetMfaFor(ADMIN_EMAIL)
  })

  test('user authenticated via CLI login can perform actions in WebUI', async ({
    page,
    request,
  }) => {
    await withCliLogin(request, async (cli) => {
      const secret = await approveCliLoginWithEnrollment(page, cli)

      // The CLI redeems the login code with its PKCE verifier
      const completion = await cli.complete(request)
      expect(completion.status).toBe(200)
      expect(completion.token).toBeTruthy()

      // The browser that approved the CLI login also holds a web session
      await page.goto(WebRoute('rack'))
      await page.waitForURL(/\/app\/rack/, { timeout: 10_000 })
      const csrfToken = await readCsrfToken(page)

      const infoResponse = await fetchJson(page, APIRoute('info'))
      expect(infoResponse.status).toBe(200)
      expect((infoResponse.data as { user?: { email?: string } })?.user?.email).toBe(ADMIN_EMAIL)

      // Step-up is valid (MFA was verified during enrollment)
      const mfaStatus = await fetchJson(page, APIRoute('auth/mfa/status'))
      expect(
        (mfaStatus.data as { recent_step_up_expires_at?: string })?.recent_step_up_expires_at
      ).toBeTruthy()

      await approveDeployRequestWithInlineMfa(page, secret, csrfToken)
    })
  })

  test('API calls after CLI login work correctly', async ({ page, request }) => {
    await withCliLogin(request, async (cli) => {
      await approveCliLoginWithEnrollment(page, cli)

      // The login code is single use
      expect((await cli.complete(request)).status).toBe(200)
      expect((await cli.complete(request)).status).toBe(400)

      const cookies = await page.context().cookies()
      expect(cookies.find((c) => c.name === 'session_token')).toBeTruthy()

      await page.goto(WebRoute('rack'))
      await page.waitForURL(/\/app\/rack/, { timeout: 10_000 })
      const csrfToken = await readCsrfToken(page)

      for (const endpoint of ['info', 'auth/mfa/status', 'deploy-approval-requests']) {
        const result = await fetchJson(page, APIRoute(endpoint))
        expect(
          result.ok,
          `Expected ${endpoint} to return ok, got status ${result.status}`
        ).toBeTruthy()
      }

      await assertDeployRequestPostAuthenticates(page, csrfToken)
    })
  })
})

async function approveDeployRequestWithInlineMfa(page: Page, secret: string, csrfToken: string) {
  const approvalPublicId = await createPendingDeployApprovalRequest()
  try {
    // The approve endpoint requires inline MFA with every request (MFAAlways). Other tests on this
    // shard may have used admin's current TOTP time step, so clear replay state first.
    await clearMfaAttempts()
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
        csrf: csrfToken,
      }
    )

    expect(
      approveResponse.ok,
      `POST to approve endpoint failed with status ${approveResponse.status}. ` +
        `Body: ${JSON.stringify(approveResponse.body)}. ` +
        'CLI login sessions should properly authenticate POST requests with MFA.'
    ).toBeTruthy()

    await page.goto(WebRoute('deploy-approval-requests'))
    await expect(page.getByRole('heading', { name: /Deploy Approvals/i })).toBeVisible({
      timeout: 10_000,
    })
    await expect(page.locator('table td:has-text("approved")').first()).toBeVisible({
      timeout: 5000,
    })
  } finally {
    await deleteDeployApprovalRequest(approvalPublicId)
  }
}

async function assertDeployRequestPostAuthenticates(page: Page, csrfToken: string) {
  // POST to an MFANone route works after CLI login (session cookie + CSRF token)
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
    { url: APIRoute('deploy-approval-requests'), csrf: csrfToken }
  )

  // A 400 validation error is acceptable: auth worked but the payload was invalid
  expect(
    postResult.status !== 401 && postResult.status !== 403,
    `POST deploy-approval-requests failed with auth error status ${postResult.status}. ` +
      `Body: ${JSON.stringify(postResult.body)}. ` +
      'This indicates CLI login sessions are not properly authenticating POST requests.'
  ).toBeTruthy()
}
