import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { authService } from './auth'
import { APIRoute } from './routes'

describe('authService.logout', () => {
  let assign: ReturnType<typeof vi.fn>

  beforeEach(() => {
    vi.restoreAllMocks()
    assign = vi.fn()
    vi.stubGlobal('location', { ...window.location, assign })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('revokes the session with a POST, then goes to the login page', async () => {
    const fetchSpy = vi
      .spyOn(globalThis as { fetch: typeof fetch }, 'fetch')
      .mockResolvedValue(new Response('{"logged_out":true}', { status: 200 }))

    await expect(authService.logout()).resolves.toBe(true)

    expect(fetchSpy).toHaveBeenCalledWith(
      APIRoute('auth/web/logout'),
      expect.objectContaining({ method: 'POST', credentials: 'include' })
    )
    expect(assign).toHaveBeenCalledWith(expect.stringMatching(/login$/))
  })

  it('stays on the page when the server did not revoke the session', async () => {
    vi.spyOn(globalThis as { fetch: typeof fetch }, 'fetch').mockResolvedValue(
      new Response('{"error":"logout_failed"}', { status: 500 })
    )

    await expect(authService.logout()).resolves.toBe(false)
    expect(assign).not.toHaveBeenCalled()
  })

  it('stays on the page when the request fails', async () => {
    vi.spyOn(globalThis as { fetch: typeof fetch }, 'fetch').mockRejectedValue(
      new TypeError('offline')
    )

    await expect(authService.logout()).resolves.toBe(false)
    expect(assign).not.toHaveBeenCalled()
  })
})
