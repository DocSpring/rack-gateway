import { describe, expect, it } from 'vitest'
import { createAppRouter, detectBasepath } from './app'

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

describe('route tree', () => {
  // Pages under the signed-in layout must match their own route, not fall through to "Not Found".
  it('matches the layout pages and the home route', () => {
    const router = createAppRouter('/app')
    const expected: Record<string, string> = {
      '/': '/',
      '/rack': '/rack',
      '/apps': '/apps',
      '/apps/web/services': '/apps/$app/services',
      '/users': '/users',
      '/account/security': '/account/security',
      '/login': '/login',
    }
    for (const [path, fullPath] of Object.entries(expected)) {
      const [, , foundRoute] = router.getMatchedRoutes(path)
      expect(foundRoute?.fullPath, path).toBe(fullPath)
    }
  })
})

const USE_PARAMS_FROM_RE = /useParams\(\{ from: '([^']+)' \}\)/g

describe('route ids', () => {
  // useParams({ from }) throws "Invariant failed" when the id doesn't exist, e.g. after the route tree changes.
  it('every useParams({ from }) in the pages names a real route', () => {
    const router = createAppRouter('/app')
    const sources = import.meta.glob('./pages/**/*.tsx', {
      query: '?raw',
      import: 'default',
      eager: true,
    })
    const ids = Object.values(sources).flatMap((source) =>
      [...String(source).matchAll(USE_PARAMS_FROM_RE)].map((match) => match[1])
    )
    expect(ids.length).toBeGreaterThan(0)
    for (const id of ids) {
      expect(router.routesById[id as keyof typeof router.routesById], id).toBeDefined()
    }
  })
})
