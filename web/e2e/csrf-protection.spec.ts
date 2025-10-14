import { expect, request, test } from '@playwright/test'
import { APIRoute, WebRoute } from '@/lib/routes'
import { login, resetMfaFor, satisfyMFAStepUpModal } from './helpers'

const ADMIN_EMAIL = 'admin@example.com'

test.describe('CSRF Protection for Proxy Routes', () => {
  test.beforeEach(async () => {
    await resetMfaFor(ADMIN_EMAIL)
  })

  test('browser with valid cookie cannot access Convox proxy routes', async ({ page }) => {
    // First, login to get a valid session cookie
    await login(page)

    // Verify we're logged in by checking we can access gateway API endpoints
    const infoResponse = await page.request.get(APIRoute('info'))
    expect(infoResponse.status()).toBe(200)
    const infoData = await infoResponse.json()
    expect(infoData.user.email).toBeTruthy()

    // Now try to access CLI-only rack-proxy routes that should reject browser cookies
    // These are routes that proxy through to the actual Convox API (CLI only)
    const cliOnlyRoutes = [
      { path: '/api/v1/rack-proxy/system', method: 'GET' },
      { path: '/api/v1/rack-proxy/apps', method: 'GET' },
      { path: '/api/v1/rack-proxy/apps/test-app/builds', method: 'POST' },
      { path: '/api/v1/rack-proxy/apps/test-app', method: 'DELETE' },
    ]

    for (const route of cliOnlyRoutes) {
      // Try to access the CLI-only route with just the cookie (no Authorization header)
      const response = await page.request.fetch(route.path, {
        method: route.method as any,
        failOnStatusCode: false,
      })

      // Should be rejected with 401 because cookies aren't allowed for CLI-only routes
      expect(response.status()).toBe(401)

      const body = await response.text()
      expect(body).toContain('browser session cookies are not permitted for CLI routes')

      // Verify the error header is set
      const errorReason = response.headers()['x-error-reason']
      expect(errorReason).toBeTruthy()
    }
  })

  test('CLI with Authorization header can access proxy routes', async ({ page }) => {
    await login(page)

    // Navigate to API tokens page
    await page.goto(WebRoute('api-tokens'))
    await expect(page.getByRole('heading', { name: /API Tokens/i })).toBeVisible()

    const tokenName = `Playwright CLI Token ${Date.now()}`
    await page.getByRole('button', { name: /Create Token/i }).click()
    const createDialog = page.getByRole('dialog')
    await expect(createDialog).toBeVisible()
    await createDialog.getByLabel('Token Name').fill(tokenName)

    // Click the CI/CD role shortcut button
    await createDialog.getByRole('button', { name: 'CI/CD' }).click()

    // Submit the form
    await createDialog.getByRole('button', { name: /Create Token/i }).click()

    // Step-up modal WILL appear because this is a sensitive operation
    await satisfyMFAStepUpModal(page, { require: true })

    // Token should be created successfully
    await expect(page.getByText(/API token created successfully/i)).toBeVisible()

    // Extract the token from the success dialog - it's in a font-mono div
    const tokenDisplay = createDialog.locator('.font-mono').filter({ hasText: /^rgw_/ }).first()
    await expect(tokenDisplay).toBeVisible()
    const apiToken = await tokenDisplay.textContent()
    expect(apiToken).toBeTruthy()
    expect(apiToken).toMatch(/^rgw_/)

    // Close the success dialog
    await page.getByRole('button', { name: /Done/i }).click()

    // Now test that the CLI can use this token to access proxy routes
    const cliContext = await request.newContext({
      baseURL: process.env.PLAYWRIGHT_BASE_URL || 'http://localhost:8447',
    })

    const response = await cliContext.get('/api/v1/rack-proxy/system', {
      headers: {
        Authorization: `Bearer ${apiToken}`,
      },
      failOnStatusCode: false,
    })

    const status = response.status()
    let body: string | null = null
    if (status === 403) {
      body = await response.text()
    }

    await cliContext.dispose()

    // Should succeed (200) or fail with permission denied (403)
    // but NOT 401 (which would indicate auth failed)
    expect(status).not.toBe(401)

    if (status === 403 && body) {
      expect(body).not.toContain('browser session cookies are not permitted for CLI routes')
    }

    // Clean up the token
    const row = page.locator('tr', { hasText: tokenName })
    await expect(row).toBeVisible()
    await row.getByRole('button', { name: /Actions for/i }).click()
    await page.getByText('Delete Token').click()
    const confirmDialog = page.getByRole('dialog')
    await confirmDialog.getByLabel('Confirmation').fill('DELETE')
    await confirmDialog.getByRole('button', { name: /Delete Token/i }).click()
    await satisfyMFAStepUpModal(page)
    await expect(row).toHaveCount(0)
  })

  test('malicious site cannot trigger Convox operations via CSRF', async ({ page }) => {
    // Login to get a valid session
    await login(page)

    // Create a malicious HTML page that tries to perform CSRF attack on CLI-only routes
    const maliciousHtml = `
      <!DOCTYPE html>
      <html>
        <head><title>Evil Site</title></head>
        <body>
          <h1>Malicious Site Attempting CSRF</h1>
          <form id="csrf-form" method="POST" action="${process.env.PLAYWRIGHT_BASE_URL || 'http://localhost:8447'}/api/v1/rack-proxy/apps/production/builds">
            <input type="hidden" name="description" value="malicious-build">
          </form>
          <script>
            // Try to submit form automatically
            document.getElementById('csrf-form').submit();
          </script>
        </body>
      </html>
    `

    // Navigate to a data URL with the malicious content
    await page.goto(`data:text/html,${encodeURIComponent(maliciousHtml)}`)

    // Wait a moment for any potential CSRF to attempt
    await page.waitForTimeout(1000)

    // Check that no successful request was made
    // The form submission should have resulted in a 401
    // We can't directly observe the form submission result, but we can
    // verify by checking console errors or network activity

    // Instead, let's do a more direct test with fetch
    const baseUrl = process.env.PLAYWRIGHT_BASE_URL || 'http://localhost:8447'
    const result = await page.evaluate(async (url) => {
      try {
        // Try to make a state-changing request using fetch (which sends cookies)
        const response = await fetch(`${url}/api/v1/rack-proxy/apps/production/builds`, {
          method: 'POST',
          credentials: 'include', // Include cookies
          headers: {
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({ description: 'malicious-build' }),
        })
        return {
          status: response.status,
          text: await response.text(),
        }
      } catch (error: any) {
        return { error: error.message }
      }
    }, baseUrl)

    // Should be rejected with 401
    expect(result).toHaveProperty('status', 401)
    if (result.text) {
      expect(result.text).toContain('browser session cookies are not permitted for CLI routes')
    }
  })

  test('gateway API endpoints still work with cookies', async ({ page }) => {
    // Login to get session cookie
    await login(page)

    // These gateway-specific endpoints SHOULD work with cookies
    const allowedEndpoints = [
      APIRoute('info'),
      APIRoute('users'),
      APIRoute('roles'),
      APIRoute('audit-logs'),
      APIRoute('api-tokens'),
    ]

    for (const endpoint of allowedEndpoints) {
      const response = await page.request.get(endpoint, {
        failOnStatusCode: false,
      })

      // These should work (200) or possibly 403 if user lacks permission
      // but NOT 401 (authentication required)
      expect([200, 403]).toContain(response.status())

      if (response.status() === 403) {
        // Permission denied is OK, but not "CLI authentication required"
        const body = await response.text()
        expect(body).not.toContain('CLI authentication required')
      }
    }
  })
})
