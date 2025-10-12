import { describe, expect, it } from 'vitest'
import { detectBasepath } from './app'

describe('detectBasepath', () => {
  it('detects /web basepath from window location', () => {
    const meta = import.meta as unknown as { env?: { BASE_URL?: string } }
    const original = meta.env
    meta.env = { BASE_URL: '/' }
    const loc = globalThis.window?.location
    const origHref = loc?.href
    const origPath = loc?.pathname
    Object.defineProperty(window, 'location', {
      value: { ...(loc || {}), href: 'http://localhost/web', pathname: '/web' },
      writable: true,
    })
    try {
      expect(detectBasepath()).toBe('/web')
    } finally {
      Object.defineProperty(window, 'location', {
        value: { ...(loc || {}), href: origHref, pathname: origPath },
        writable: true,
      })
      const m2 = import.meta as unknown as { env?: { BASE_URL?: string } }
      m2.env = original
    }
  })
})
