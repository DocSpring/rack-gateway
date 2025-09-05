import { beforeEach, describe, expect, it, vi } from 'vitest'
import { authService } from './auth'

describe('authService', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('logout calls server logout endpoint', async () => {
    const fetchSpy = vi
      .spyOn(globalThis as { fetch: typeof fetch }, 'fetch')
      .mockResolvedValue(new Response() as unknown as Response)
    authService.logout()
    // Allow promise in finally to run
    await Promise.resolve()

    expect(fetchSpy).toHaveBeenCalledWith('/api/.gateway/web/logout', { credentials: 'include' })
  })
})
