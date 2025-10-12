import { writeFileSync } from 'node:fs'
import { test as base, expect as playwrightExpect } from '@playwright/test'
import { format } from 'prettier'
import { APIRoute, WebRoute } from '@/lib/routes'

export const test = base.extend({
  page: async ({ page }, use, testInfo) => {
    // Mock WebAuthn API to prevent real hardware calls in E2E tests
    await page.addInitScript(() => {
      // Override navigator.credentials to return mock responses
      if (navigator.credentials) {
        const originalGet = navigator.credentials.get.bind(navigator.credentials)
        const originalCreate = navigator.credentials.create.bind(navigator.credentials)

        navigator.credentials.get = async (options?: CredentialRequestOptions) => {
          // Check if this is a WebAuthn request
          if (options && 'publicKey' in options) {
            // Return mock credential for assertion - backend E2E_TEST_MODE will handle it
            const mockCredential = {
              id: 'e2e-mock-credential-id',
              type: 'public-key',
              rawId: new Uint8Array([1, 2, 3, 4, 5, 6, 7, 8]).buffer,
              authenticatorAttachment: null,
              response: {
                clientDataJSON: new Uint8Array([10, 11, 12, 13]).buffer,
                authenticatorData: new Uint8Array([20, 21, 22, 23]).buffer,
                signature: new Uint8Array([30, 31, 32, 33]).buffer,
                userHandle: null,
              },
              getClientExtensionResults: () => ({}),
              toJSON: () => ({ id: 'e2e-mock-credential-id', type: 'public-key' }),
            } as PublicKeyCredential
            return Promise.resolve(mockCredential)
          }
          return await originalGet(options)
        }

        navigator.credentials.create = async (options?: CredentialCreationOptions) => {
          if (options && 'publicKey' in options) {
            // Return mock credential for registration
            const mockCredential = {
              id: 'e2e-mock-credential-id',
              type: 'public-key',
              rawId: new Uint8Array([1, 2, 3, 4, 5, 6, 7, 8]).buffer,
              authenticatorAttachment: null,
              response: {
                clientDataJSON: new Uint8Array([10, 11, 12, 13]).buffer,
                attestationObject: new Uint8Array([20, 21, 22, 23]).buffer,
              },
              getClientExtensionResults: () => ({}),
              toJSON: () => ({ id: 'e2e-mock-credential-id', type: 'public-key' }),
            } as PublicKeyCredential
            return Promise.resolve(mockCredential)
          }
          return await originalCreate(options)
        }
      }
    })

    const errors: string[] = []

    await page.addInitScript(() => {
      const globalObject = window as unknown as Record<string, unknown>
      globalObject.__e2e_last_mfa_enroll = null

      const originalFetch = window.fetch.bind(window)
      window.fetch = async (...args) => {
        const response = await originalFetch(...args)
        try {
          const requestInfo = args[0]
          let url = ''
          if (typeof requestInfo === 'string') {
            url = requestInfo
          } else if (requestInfo instanceof Request) {
            url = requestInfo.url
          }
          if (
            typeof url === 'string' &&
            url.includes('/auth/mfa/enroll/totp/start') &&
            response.ok
          ) {
            console.debug('[E2E] Intercepting TOTP enrollment response via fetch', url)
            response
              .clone()
              .json()
              .then((data) => {
                globalObject.__e2e_last_mfa_enroll = data
              })
              .catch((err) => console.error('Failed to capture fetch TOTP response', err))
          }
        } catch (err) {
          console.error('Failed to inspect fetch request', err)
        }
        return response
      }

      const originalOpen = XMLHttpRequest.prototype.open
      XMLHttpRequest.prototype.open = function (method: string, url: string, ...rest) {
        ;(this as any).__e2e_capture_totp =
          typeof method === 'string' &&
          method.toUpperCase() === 'POST' &&
          typeof url === 'string' &&
          url.includes('/auth/mfa/enroll/totp/start')
        if ((this as any).__e2e_capture_totp) {
          console.debug('[E2E] Observing TOTP enrollment request via XHR', method, url)
        }
        return originalOpen.call(this, method, url, ...rest)
      }

      const originalSend = XMLHttpRequest.prototype.send
      XMLHttpRequest.prototype.send = function (...args) {
        if ((this as any).__e2e_capture_totp) {
          this.addEventListener('load', function () {
            try {
              const xhr = this as XMLHttpRequest
              const raw =
                xhr.responseType && xhr.responseType !== 'text' ? xhr.response : xhr.responseText
              if (!raw) return
              const parsed = typeof raw === 'string' ? JSON.parse(raw) : raw
              globalObject.__e2e_last_mfa_enroll = parsed
              console.debug('[E2E] Captured TOTP enrollment response via XHR')
            } catch (err) {
              console.error('Failed to capture TOTP enrollment response', err)
            }
          })
        }
        return originalSend.apply(this, args)
      }
    })

    // Log 4xx/5xx responses with URL and a short body snippet
    page.on('response', async (resp) => {
      const status = resp.status()
      if (status >= 400) {
        const req = resp.request()
        const method = req.method()
        const url = resp.url()
        const debugAuth = (process.env.LOG_LEVEL || '').toLowerCase() === 'debug'
        if (status === 401 && url.includes(APIRoute('me')) && !debugAuth) {
          return
        }

        // Suppress expected 401s from CLI-only proxy endpoints
        if (status === 401 && !url.includes('/.gateway/')) {
          return
        }

        let body = ''
        try {
          body = await resp.text()
        } catch {}
        const snippet = body ? body.slice(0, 200).replace(/\s+/g, ' ').trim() : ''

        if (status === 403 && snippet.includes('mfa_enrollment_required')) {
          return
        }

        if (status === 401 && url.includes(APIRoute('me'))) return

        // Suppress expected 400s from WebAuthn verify when no method enrolled (E2E mode)
        if (status === 400 && url.includes('/mfa/webauthn/verify')) {
          return
        }

        console.log(`[resp ${status}] ${method} ${url}${snippet ? ` body="${snippet}"` : ''}`)
      }
    })
    page.on('console', (msg) => {
      const text = msg.text()
      // Suppress generic 401 console noise entirely (expected before login)
      const isGeneric401 =
        /the server responded with a status of 401 \(Unauthorized\)\s*$/i.test(text) ||
        /\b401\b/i.test(text)
      if (isGeneric401) return
      // Suppress generic 403 console noise (expected MFA/auth checks)
      const isGeneric403 = /status of 403 \(Forbidden\)/i.test(text)
      if (isGeneric403) return
      if (msg.type() === 'debug' && text.includes('[vite] connect')) return
      if (
        msg.type() === 'info' &&
        text.includes('Download the React DevTools for a better development experience')
      )
        return

      // For CSP violations, print full error with stack trace
      if (msg.type() === 'error' && /Content Security Policy|CSP/i.test(text)) {
        console.log('console:', msg.type(), text)
        const loc = msg.location()
        console.log(`  at ${loc.url}:${loc.lineNumber}:${loc.columnNumber}`)
      } else {
        // Always print console logs for diagnosis (after suppression checks)
        console.log('console:', msg.type(), text)
      }

      if (msg.type() === 'error') {
        // Ignore all 401/Unauthorized console errors (expected before login)
        const is401 = /\b401\b/i.test(text) || /Unauthorized/i.test(text)
        if (is401) return
        if (/mfa_enrollment_required/i.test(text)) return
        // Ignore generic 400 errors (likely from WebAuthn when no method enrolled in E2E mode)
        const isGeneric400 = /status of 400 \(Bad Request\)/i.test(text)
        if (isGeneric400) return
        const isDevModule502 =
          /status of 502 \(Bad Gateway\)/i.test(text) && text.includes(WebRoute('/'))
        if (isDevModule502) return
        errors.push(`console.${msg.type()}: ${text}`)
      }
    })
    page.on('pageerror', (err) => {
      console.log('pageerror:', err)
      errors.push(`pageerror: ${String(err)}`)
    })
    await use(page)

    // Save HTML snapshot on failure
    if (testInfo.status !== testInfo.expectedStatus) {
      const htmlPath = testInfo.outputPath('page-content.html')
      const html = await page.content()
      // Format HTML for readability
      const formatted = await format(html, { parser: 'html', printWidth: 120 })
      writeFileSync(htmlPath, formatted)
      testInfo.attachments.push({ name: 'page-html', path: htmlPath, contentType: 'text/html' })
    }

    if (errors.length > 0) {
      throw new Error(`JS errors detected during test:\n${errors.join('\n')}`)
    }
  },
})

export const expect = playwrightExpect
