import { beforeEach, describe, expect, it, vi } from 'vitest'
import { authService } from './auth'

describe('authService', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('logout calls server logout endpoint', () => {
    // Keep fetch pending to avoid triggering navigation in .finally()
    const fetchSpy = vi.spyOn(globalThis as { fetch: typeof fetch }, 'fetch').mockImplementation(
      () =>
        new Promise(() => {
          /* keep pending to avoid redirect in test */
        }) as unknown as Promise<Response>
    )
    authService.logout()
    expect(fetchSpy).toHaveBeenCalledWith('/.gateway/api/web/logout', { credentials: 'include' })
  })
})
