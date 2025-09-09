import { test as base, expect } from '@playwright/test'

export const test = base.extend({
  page: async ({ page }, use) => {
    const errors: string[] = []
    let lastMe401At = 0
    // Log 4xx/5xx responses with URL and a short body snippet
    page.on('response', async (resp) => {
      const status = resp.status()
      if (status >= 400) {
        const req = resp.request()
        const method = req.method()
        const url = resp.url()
        const debugAuth = process.env.GATEWAY_DEBUG_AUTH === 'true'
        if (status === 401 && url.includes('/.gateway/api/me') && !debugAuth) {
          return
        }
        let body = ''
        try { body = await resp.text() } catch {}
        const snippet = body ? body.slice(0, 200).replace(/\s+/g, ' ').trim() : ''
        console.log(`[resp ${status}] ${method} ${url}${snippet ? ' body="' + snippet + '"' : ''}`)
        if (status === 401 && url.includes('/.gateway/api/me')) {
          lastMe401At = Date.now()
        }
      }
    })
    page.on('console', (msg) => {
      const text = msg.text()
      const url = page.url()
      // Suppress generic 401 console noise entirely (expected before login)
      const isGeneric401 = /the server responded with a status of 401 \(Unauthorized\)\s*$/i.test(text) || /\b401\b/i.test(text)
      if (isGeneric401) return
      // Always print console logs for diagnosis (after suppression checks)
      console.log('console:', msg.type(), text)
      if (msg.type() === 'error') {
        // Ignore all 401/Unauthorized console errors (expected before login)
        const is401 = /\b401\b/i.test(text) || /Unauthorized/i.test(text)
        if (is401) return
        errors.push(`console.${msg.type()}: ${text}`)
      }
    })
    page.on('pageerror', (err) => {
      console.log('pageerror:', err)
      errors.push(`pageerror: ${String(err)}`)
    })
    await use(page)
    if (errors.length > 0) {
      throw new Error('JS errors detected during test:\n' + errors.join('\n'))
    }
  },
})

export { expect }
