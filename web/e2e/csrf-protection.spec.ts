import { expect, test } from '@playwright/test'
import { login } from './helpers'

test.describe('CSRF Protection for Proxy Routes', () => {
  test('browser with valid cookie cannot access Convox proxy routes', async ({ page }) => {
    // First, login to get a valid session cookie
    await login(page)

    // Verify we're logged in by checking we can access gateway API endpoints
    const meResponse = await page.request.get('/.gateway/api/me')
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
      expect(body).toContain('CLI authentication required')

      // Verify the error header is set
      const errorReason = response.headers()['x-error-reason']
      expect(errorReason).toBeTruthy()
    }
  })

  test('CLI with Authorization header can access proxy routes', async ({ page }) => {
    // This test simulates what the CLI does - using Authorization header instead of cookies

    // First, get a valid token via the CLI login flow
    // In a real test, we'd use the actual CLI login endpoint
    // For this test, we'll login via UI and extract the token
    await login(page)

    // Get the token from the cookie (in real scenario, CLI would have this from login)
    const cookies = await page.context().cookies()
    const tokenCookie = cookies.find((c) => c.name === 'gateway_token')
    expect(tokenCookie).toBeTruthy()
    const token = tokenCookie!.value

    // Now make requests with Authorization header (like CLI does)
    const response = await page.request.get('/system', {
      headers: {
        Authorization: `Bearer ${token}`,
      },
      failOnStatusCode: false,
    })

    // This should work (assuming valid token and proper permissions)
    // It will either succeed or fail with permission error, not auth error
    expect(response.status()).not.toBe(401)

    // If it's 403, it's a permission issue, not an auth issue
    if (response.status() === 403) {
      const body = await response.text()
      expect(body).not.toContain('CLI authentication required')
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
    const result = await page.evaluate(async () => {
      try {
        // Try to make a state-changing request using fetch (which sends cookies)
        const response = await fetch('/apps/production/builds', {
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
    })

    // Should be rejected with 401
    expect(result).toHaveProperty('status', 401)
    if (result.text) {
      expect(result.text).toContain('CLI authentication required')
    }
  })

  test('gateway API endpoints still work with cookies', async ({ page }) => {
    // Login to get session cookie
    await login(page)

    // These gateway-specific endpoints SHOULD work with cookies
    const allowedEndpoints = [
      '/.gateway/api/me',
      '/.gateway/api/admin/users',
      '/.gateway/api/admin/roles',
      '/.gateway/api/admin/audit',
      '/.gateway/api/admin/tokens',
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
