import axios from 'axios'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { authService } from './auth'

vi.mock('axios')

describe('getCurrentUser', () => {
  beforeEach(() => {
    vi.resetAllMocks()
  })

  it('uses withCredentials and returns user', async () => {
    const mockResp = { data: { email: 'admin@example.com', name: 'Admin' } }
    vi.mocked(axios.get).mockResolvedValueOnce(mockResp as unknown as never)

    const user = await authService.getCurrentUser()
    expect(axios.get).toHaveBeenCalledWith('/.gateway/api/me', { withCredentials: true })
    expect(user?.email).toBe('admin@example.com')
  })

  it('returns null on error', async () => {
    vi.mocked(axios.get).mockRejectedValueOnce(new Error('nope'))
    const user = await authService.getCurrentUser()
    expect(user).toBeNull()
  })

  it('suppresses 401 auth error storage when requested', async () => {
    const setItem = vi.spyOn(window.sessionStorage, 'setItem')
    vi.mocked(axios.get).mockRejectedValueOnce({
      response: { status: 401 },
    } as never)

    const user = await authService.getCurrentUser({ suppressAuthError: true })

    expect(user).toBeNull()
    expect(setItem).not.toHaveBeenCalled()
    setItem.mockRestore()
  })
})
