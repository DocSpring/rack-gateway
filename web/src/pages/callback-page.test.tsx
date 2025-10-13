import { render, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { CallbackPage } from './callback-page'

// Mock TanStack Router hooks used by the component
const mockNavigate = vi.fn()
vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = (await importOriginal()) as Record<string, unknown>
  return {
    ...actual,
    useNavigate: () => mockNavigate,
    useRouter: () => ({
      history: { location: { search: '?code=abc&state=123' } },
    }),
  }
})

// Mock auth service to resolve successfully
vi.mock('../lib/auth', () => ({
  authService: {
    handleCallback: vi.fn().mockResolvedValue({}),
  },
}))

describe('CallbackPage', () => {
  it('navigates to /rack after successful callback', async () => {
    render(<CallbackPage />)
    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith({ to: '/rack', replace: true })
    })
  })
})
