import { describe, expect, it, vi, beforeEach } from 'vitest'
import { authService } from './auth'

describe('authService', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('logout calls server logout endpoint', async () => {
    const fetchSpy = vi.spyOn(global, 'fetch' as any).mockResolvedValue({} as any)
    authService.logout()
    // Allow promise in finally to run
    await Promise.resolve()

    expect(fetchSpy).toHaveBeenCalledWith('/api/.gateway/web/logout', { credentials: 'include' })
  })
})
