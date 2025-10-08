import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import { getMFAStatus, verifyCliMfa, verifyMFA } from '@/lib/api'
import { MFAChallengePage } from './mfa-challenge-page'

vi.mock('@/lib/api', () => ({
  getMFAStatus: vi.fn(),
  verifyCliMfa: vi.fn(),
  verifyMFA: vi.fn(),
  startWebAuthnAssertion: vi.fn(),
  verifyWebAuthnAssertion: vi.fn(),
}))

vi.mock('@/components/ui/use-toast', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}))

const createWrapper = () => {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
  return ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
}

describe('MFAChallengePage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(getMFAStatus).mockResolvedValue({
      enrolled: true,
      required: false,
      methods: [{ id: 1, type: 'totp', label: 'Authenticator', created_at: '2024-01-01' }],
      trusted_devices: [],
      backup_codes: { total: 0, unused: 0 },
      webauthn_available: false,
    })

    // Mock window.location.assign
    vi.stubGlobal('location', {
      search: '?mode=cli&state=test-state',
      assign: vi.fn(),
    })
  })

  describe('Auto-submit on paste', () => {
    it('should auto-submit when pasting a 6-digit code', async () => {
      vi.mocked(verifyCliMfa).mockResolvedValue({ redirect: 'http://localhost' })

      const Wrapper = createWrapper()
      render(<MFAChallengePage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByLabelText('Verification code')).toBeInTheDocument()
      })

      const input = screen.getByLabelText('Verification code')

      // Simulate pasting a 6-digit code
      fireEvent.paste(input, {
        clipboardData: {
          getData: () => '123456',
        },
      })

      // Trigger the onChange that happens after paste
      fireEvent.change(input, { target: { value: '123456' } })

      await waitFor(() => {
        expect(verifyCliMfa).toHaveBeenCalledWith({
          state: 'test-state',
          code: '123456',
        })
      })
    })

    it('should auto-submit when typing the 6th digit', async () => {
      vi.mocked(verifyCliMfa).mockResolvedValue({ redirect: 'http://localhost' })

      const Wrapper = createWrapper()
      render(<MFAChallengePage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByLabelText('Verification code')).toBeInTheDocument()
      })

      const input = screen.getByLabelText('Verification code')

      // Type a 6-digit code
      fireEvent.change(input, { target: { value: '654321' } })

      await waitFor(() => {
        expect(verifyCliMfa).toHaveBeenCalledWith({
          state: 'test-state',
          code: '654321',
        })
      })
    })

    it('should not auto-submit with less than 6 digits', async () => {
      vi.mocked(verifyCliMfa).mockResolvedValue({ redirect: 'http://localhost' })

      const Wrapper = createWrapper()
      render(<MFAChallengePage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByLabelText('Verification code')).toBeInTheDocument()
      })

      const input = screen.getByLabelText('Verification code')

      // Type only 5 digits
      fireEvent.change(input, { target: { value: '12345' } })

      // Wait a bit to ensure no call was made
      await new Promise((resolve) => setTimeout(resolve, 100))

      expect(verifyCliMfa).not.toHaveBeenCalled()
    })

    it('should not auto-submit with non-numeric characters', async () => {
      vi.mocked(verifyCliMfa).mockResolvedValue({ redirect: 'http://localhost' })

      const Wrapper = createWrapper()
      render(<MFAChallengePage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByLabelText('Verification code')).toBeInTheDocument()
      })

      const input = screen.getByLabelText('Verification code')

      // Type 6 characters but with a letter
      fireEvent.change(input, { target: { value: '12345a' } })

      // Wait a bit to ensure no call was made
      await new Promise((resolve) => setTimeout(resolve, 100))

      expect(verifyCliMfa).not.toHaveBeenCalled()
    })
  })

  describe('Web mode', () => {
    beforeEach(() => {
      vi.stubGlobal('location', {
        search: '?mode=web',
        assign: vi.fn(),
      })
    })

    it('should auto-submit for web mode', async () => {
      vi.mocked(verifyMFA).mockResolvedValue({
        mfa_verified_at: '2024-01-01T00:00:00Z',
        recent_step_up_expires_at: '2024-01-01T00:15:00Z',
        trusted_device_cookie: false,
      })

      const Wrapper = createWrapper()
      render(<MFAChallengePage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByLabelText('Verification code')).toBeInTheDocument()
      })

      const input = screen.getByLabelText('Verification code')

      // Type a 6-digit code
      fireEvent.change(input, { target: { value: '999888' } })

      await waitFor(() => {
        expect(verifyMFA).toHaveBeenCalledWith({
          code: '999888',
          trust_device: true,
        })
      })
    })
  })
})
