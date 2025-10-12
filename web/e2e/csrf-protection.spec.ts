import { expect, request, test } from '@playwright/test'
import { authenticator } from 'otplib'
import { APIRoute, WebRoute } from '@/lib/routes'
import { clearMfaAttempts, getUserMfaSecret } from './db'
import { login } from './helpers'

test.describe('CSRF Protection for Proxy Routes', () => {
  test('browser with valid cookie cannot access Convox proxy routes', async ({ page }) => {
    // First, login to get a valid session cookie
    await login(page)

    // Verify we're logged in by checking we can access gateway API endpoints
    const meResponse = await page.request.get(APIRoute('me'))
    expect(meResponse.status()).toBe(200)
    const meData = await meResponse.json()
    expect(meData.email).toBeTruthy()

    // Now try to access Convox proxy routes that should be CLI-only
    // These are routes that proxy through to the actual Convox API
    const proxyRoutes = [
      { path: '/system', method: 'GET' },
      { path: '/apps', method: 'GET' },
      { path: '/racks', method: 'GET' },
      { path: '/apps/test-app/processes', method: 'GET' },
      { path: '/apps/test-app/builds', method: 'POST' },
      { path: '/apps/test-app', method: 'DELETE' },
    ]

    for (const route of proxyRoutes) {
      // Try to access the proxy route with just the cookie (no Authorization header)
      const response = await page.request.fetch(route.path, {
        method: route.method as any,
        failOnStatusCode: false,
      })

      // Should be rejected with 401 because cookies aren't allowed for proxy routes
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

    // Navigate to the SPA shell to read the injected CSRF meta tag (single source of truth)
    await page.goto(WebRoute('/'), { waitUntil: 'networkidle' })
    const csrfTokenHandle = await page.waitForFunction(
      () => {
        const value = document
          .querySelector('meta[name="rgw-csrf-token"]')
          ?.getAttribute('content')
          ?.trim()
        if (!value || value === 'RGW_CSRF_TOKEN') {
          return null
        }
        return value
      },
      undefined,
      { timeout: 5000 }
    )
    const csrfToken = (await csrfTokenHandle.jsonValue()) as string | null
    expect(csrfToken, 'expected CSRF meta tag to be present').toBeTruthy()
    if (!csrfToken) {
      throw new Error('CSRF token not found in meta tag')
    }

    // Get MFA secret for generating step-up code
    const mfaSecret = await getUserMfaSecret('admin@example.com')
    if (!mfaSecret) {
      throw new Error('MFA secret not found for admin user')
    }

    // Wait for fresh TOTP window to avoid replay protection
    const currentSecond = Math.floor(Date.now() / 1000)
    const secondsIntoWindow = currentSecond % 30
    if (secondsIntoWindow > 25) {
      // Less than 5 seconds left, wait for next window
      await new Promise((resolve) => setTimeout(resolve, (30 - secondsIntoWindow + 2) * 1000))
    }

    // Clear attempts and generate fresh code
    await clearMfaAttempts()
    // Small delay to ensure database transaction commits
    await new Promise((resolve) => setTimeout(resolve, 100))
    const mfaCode = authenticator.generate(mfaSecret)

    const tokenName = `Playwright CLI Token ${Date.now()}`
    const createResponse = await page.request.post(APIRoute('admin/tokens'), {
      headers: {
        'Content-Type': 'application/json',
        'X-CSRF-Token': csrfToken,
        'X-MFA-TOTP': mfaCode,
      },
      data: {
        name: tokenName,
        role: 'cicd',
      },
      failOnStatusCode: false,
    })

    expect(createResponse.status()).toBe(200)
    const { token: apiToken, api_token: apiTokenMeta } = (await createResponse.json()) as {
      token: string
      api_token: { id: number }
    }
    expect(apiToken).toMatch(/^rgw_/)

    const cliContext = await request.newContext({
      baseURL: process.env.PLAYWRIGHT_BASE_URL || 'http://localhost:8447',
    })

    const response = await cliContext.get('/system', {
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

    expect(status).not.toBe(401)

    if (status === 403 && body) {
      expect(body).not.toContain('browser session cookies are not permitted for CLI routes')
    }

    // Clean up the token to avoid polluting the test environment
    if (apiTokenMeta?.id) {
      await page.request.delete(APIRoute(`admin/tokens/${apiTokenMeta.id}`), {
        headers: {
          'X-CSRF-Token': csrfToken,
        },
        failOnStatusCode: false,
      })
    }
  })

  test('malicious site cannot trigger Convox operations via CSRF', async ({ page }) => {
    // Login to get a valid session
    await login(page)

    // Create a malicious HTML page that tries to perform CSRF attack
    const maliciousHtml = `
      <!DOCTYPE html>
      <html>
        <head><title>Evil Site</title></head>
        <body>
          <h1>Malicious Site Attempting CSRF</h1>
          <form id="csrf-form" method="POST" action="${process.env.PLAYWRIGHT_BASE_URL || 'http://localhost:8447'}/apps/production/builds">
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
        const response = await fetch(`${url}/apps/production/builds`, {
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
      APIRoute('me'),
      APIRoute('admin/users'),
      APIRoute('admin/roles'),
      APIRoute('admin/audit'),
      APIRoute('admin/tokens'),
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
