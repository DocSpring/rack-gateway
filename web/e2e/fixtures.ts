import { test as base, expect as playwrightExpect } from '@playwright/test'
import { APIRoute, WebRoute } from '@/lib/routes'

export const test = base.extend({
  page: async ({ page }, use) => {
    const errors: string[] = []
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

        if (status === 401 && url.includes(APIRoute('me'))) return

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
      if (msg.type() === 'debug' && text.includes('[vite] connect')) return
      if (
        msg.type() === 'info' &&
        text.includes('Download the React DevTools for a better development experience')
      )
        return

      // Always print console logs for diagnosis (after suppression checks)
      console.log('console:', msg.type(), text)
      if (msg.type() === 'error') {
        // Ignore all 401/Unauthorized console errors (expected before login)
        const is401 = /\b401\b/i.test(text) || /Unauthorized/i.test(text)
        if (is401) return
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
    if (errors.length > 0) {
      throw new Error(`JS errors detected during test:\n${errors.join('\n')}`)
    }
  },
})

export const expect = playwrightExpect
