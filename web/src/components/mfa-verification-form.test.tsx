import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import { getMFAStatus } from '@/lib/api'
import { MFAVerificationForm } from './mfa-verification-form'

vi.mock('@/lib/api', () => ({
  getMFAStatus: vi.fn(),
  startWebAuthnAssertion: vi.fn(),
  verifyWebAuthnAssertion: vi.fn(),
}))

vi.mock('@/lib/webauthn-utils', () => ({
  prepareRequestOptions: vi.fn((options) => options),
  getCredential: vi.fn(),
  serializeAssertionCredential: vi.fn(() => ({ id: 'test-credential' })),
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

describe('MFAVerificationForm', () => {
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
  })

  describe('TOTP Auto-submit', () => {
    it('should auto-submit when pasting a 6-digit code', async () => {
      const onVerify = vi.fn().mockResolvedValue(undefined)

      const Wrapper = createWrapper()
      render(
        <MFAVerificationForm onVerify={onVerify}>
          {({ TOTPInput }) => <div>{TOTPInput}</div>}
        </MFAVerificationForm>,
        { wrapper: Wrapper }
      )

      await waitFor(() => {
        expect(screen.getByLabelText('Verification code')).toBeInTheDocument()
      })

      const input = screen.getByLabelText('Verification code')

      // Simulate pasting
      fireEvent.paste(input, {
        clipboardData: {
          getData: () => '123456',
        },
      })

      // Trigger onChange that happens after paste
      fireEvent.change(input, { target: { value: '123456' } })

      await waitFor(() => {
        expect(onVerify).toHaveBeenCalledWith({
          method: 'totp',
          code: '123456',
          trust_device: true,
        })
      })
    })

    it('should auto-submit when typing the 6th digit', async () => {
      const onVerify = vi.fn().mockResolvedValue(undefined)

      const Wrapper = createWrapper()
      render(
        <MFAVerificationForm onVerify={onVerify}>
          {({ TOTPInput }) => <div>{TOTPInput}</div>}
        </MFAVerificationForm>,
        { wrapper: Wrapper }
      )

      await waitFor(() => {
        expect(screen.getByLabelText('Verification code')).toBeInTheDocument()
      })

      const input = screen.getByLabelText('Verification code')

      // Type a 6-digit code
      fireEvent.change(input, { target: { value: '654321' } })

      await waitFor(() => {
        expect(onVerify).toHaveBeenCalledWith({
          method: 'totp',
          code: '654321',
          trust_device: true,
        })
      })
    })

    it('should not auto-submit with less than 6 digits', async () => {
      const onVerify = vi.fn().mockResolvedValue(undefined)

      const Wrapper = createWrapper()
      render(
        <MFAVerificationForm onVerify={onVerify}>
          {({ TOTPInput }) => <div>{TOTPInput}</div>}
        </MFAVerificationForm>,
        { wrapper: Wrapper }
      )

      await waitFor(() => {
        expect(screen.getByLabelText('Verification code')).toBeInTheDocument()
      })

      const input = screen.getByLabelText('Verification code')

      // Type only 5 digits
      fireEvent.change(input, { target: { value: '12345' } })

      // Wait a bit
      await new Promise((resolve) => setTimeout(resolve, 100))

      expect(onVerify).not.toHaveBeenCalled()
    })

    it('should not auto-submit with non-numeric characters', async () => {
      const onVerify = vi.fn().mockResolvedValue(undefined)

      const Wrapper = createWrapper()
      render(
        <MFAVerificationForm onVerify={onVerify}>
          {({ TOTPInput }) => <div>{TOTPInput}</div>}
        </MFAVerificationForm>,
        { wrapper: Wrapper }
      )

      await waitFor(() => {
        expect(screen.getByLabelText('Verification code')).toBeInTheDocument()
      })

      const input = screen.getByLabelText('Verification code')

      // Type 6 characters with a letter
      fireEvent.change(input, { target: { value: '12345a' } })

      // Wait a bit
      await new Promise((resolve) => setTimeout(resolve, 100))

      expect(onVerify).not.toHaveBeenCalled()
    })
  })

  describe('Trust device', () => {
    it('should use trust_device=true by default', async () => {
      const onVerify = vi.fn().mockResolvedValue(undefined)

      const Wrapper = createWrapper()
      render(
        <MFAVerificationForm onVerify={onVerify}>
          {({ TOTPInput }) => <div>{TOTPInput}</div>}
        </MFAVerificationForm>,
        { wrapper: Wrapper }
      )

      await waitFor(() => {
        expect(screen.getByLabelText('Verification code')).toBeInTheDocument()
      })

      const input = screen.getByLabelText('Verification code')
      fireEvent.change(input, { target: { value: '111111' } })

      await waitFor(() => {
        expect(onVerify).toHaveBeenCalledWith({
          method: 'totp',
          code: '111111',
          trust_device: true,
        })
      })
    })

    it('should respect trustDeviceDefault=false', async () => {
      const onVerify = vi.fn().mockResolvedValue(undefined)

      const Wrapper = createWrapper()
      render(
        <MFAVerificationForm onVerify={onVerify} trustDeviceDefault={false}>
          {({ TOTPInput }) => <div>{TOTPInput}</div>}
        </MFAVerificationForm>,
        { wrapper: Wrapper }
      )

      await waitFor(() => {
        expect(screen.getByLabelText('Verification code')).toBeInTheDocument()
      })

      const input = screen.getByLabelText('Verification code')
      fireEvent.change(input, { target: { value: '222222' } })

      await waitFor(() => {
        expect(onVerify).toHaveBeenCalledWith({
          method: 'totp',
          code: '222222',
          trust_device: false,
        })
      })
    })

    it('should allow changing trust device setting', async () => {
      const onVerify = vi.fn().mockResolvedValue(undefined)

      const Wrapper = createWrapper()
      render(
        <MFAVerificationForm onVerify={onVerify}>
          {({ TOTPInput, TrustDeviceCheckbox }) => (
            <div>
              {TOTPInput}
              {TrustDeviceCheckbox}
            </div>
          )}
        </MFAVerificationForm>,
        { wrapper: Wrapper }
      )

      await waitFor(() => {
        expect(screen.getByLabelText('Verification code')).toBeInTheDocument()
      })

      // Uncheck trust device
      const checkbox = screen.getByRole('checkbox')
      fireEvent.click(checkbox)

      const input = screen.getByLabelText('Verification code')
      fireEvent.change(input, { target: { value: '333333' } })

      await waitFor(() => {
        expect(onVerify).toHaveBeenCalledWith({
          method: 'totp',
          code: '333333',
          trust_device: false,
        })
      })
    })
  })

  describe('Method selection', () => {
    it('should default to TOTP when only TOTP is available', async () => {
      vi.mocked(getMFAStatus).mockResolvedValue({
        enrolled: true,
        required: false,
        methods: [{ id: 1, type: 'totp', label: 'Authenticator', created_at: '2024-01-01' }],
        trusted_devices: [],
        backup_codes: { total: 0, unused: 0 },
        webauthn_available: false,
      })

      const onVerify = vi.fn()

      const Wrapper = createWrapper()
      render(
        <MFAVerificationForm onVerify={onVerify}>
          {({ useWebAuthn, hasTOTP, hasWebAuthn }) => (
            <div>
              <span data-testid="use-webauthn">{String(useWebAuthn)}</span>
              <span data-testid="has-totp">{String(hasTOTP)}</span>
              <span data-testid="has-webauthn">{String(hasWebAuthn)}</span>
            </div>
          )}
        </MFAVerificationForm>,
        { wrapper: Wrapper }
      )

      await waitFor(() => {
        expect(screen.getByTestId('use-webauthn')).toHaveTextContent('false')
        expect(screen.getByTestId('has-totp')).toHaveTextContent('true')
        expect(screen.getByTestId('has-webauthn')).toHaveTextContent('false')
      })
    })

    it('should default to WebAuthn when only WebAuthn is available', async () => {
      vi.mocked(getMFAStatus).mockResolvedValue({
        enrolled: true,
        required: false,
        methods: [{ id: 1, type: 'webauthn', label: 'Security Key', created_at: '2024-01-01' }],
        trusted_devices: [],
        backup_codes: { total: 0, unused: 0 },
        webauthn_available: true,
      })

      const onVerify = vi.fn()

      const Wrapper = createWrapper()
      render(
        <MFAVerificationForm onVerify={onVerify}>
          {({ useWebAuthn, hasTOTP, hasWebAuthn }) => (
            <div>
              <span data-testid="use-webauthn">{String(useWebAuthn)}</span>
              <span data-testid="has-totp">{String(hasTOTP)}</span>
              <span data-testid="has-webauthn">{String(hasWebAuthn)}</span>
            </div>
          )}
        </MFAVerificationForm>,
        { wrapper: Wrapper }
      )

      await waitFor(() => {
        expect(screen.getByTestId('use-webauthn')).toHaveTextContent('true')
        expect(screen.getByTestId('has-totp')).toHaveTextContent('false')
        expect(screen.getByTestId('has-webauthn')).toHaveTextContent('true')
      })
    })

    it('should respect preferredMethod prop', async () => {
      vi.mocked(getMFAStatus).mockResolvedValue({
        enrolled: true,
        required: false,
        methods: [
          { id: 1, type: 'totp', label: 'Authenticator', created_at: '2024-01-01' },
          { id: 2, type: 'webauthn', label: 'Security Key', created_at: '2024-01-01' },
        ],
        trusted_devices: [],
        backup_codes: { total: 0, unused: 0 },
        webauthn_available: true,
      })

      const onVerify = vi.fn()

      const Wrapper = createWrapper()
      render(
        <MFAVerificationForm onVerify={onVerify} preferredMethod="webauthn">
          {({ useWebAuthn }) => (
            <div>
              <span data-testid="use-webauthn">{String(useWebAuthn)}</span>
            </div>
          )}
        </MFAVerificationForm>,
        { wrapper: Wrapper }
      )

      await waitFor(() => {
        expect(screen.getByTestId('use-webauthn')).toHaveTextContent('true')
      })
    })

    it('should respect server preferred_method when set to auto', async () => {
      vi.mocked(getMFAStatus).mockResolvedValue({
        enrolled: true,
        required: false,
        preferred_method: 'webauthn',
        methods: [
          { id: 1, type: 'totp', label: 'Authenticator', created_at: '2024-01-01' },
          { id: 2, type: 'webauthn', label: 'Security Key', created_at: '2024-01-01' },
        ],
        trusted_devices: [],
        backup_codes: { total: 0, unused: 0 },
        webauthn_available: true,
      })

      const onVerify = vi.fn()

      const Wrapper = createWrapper()
      render(
        <MFAVerificationForm onVerify={onVerify} preferredMethod="auto">
          {({ useWebAuthn }) => (
            <div>
              <span data-testid="use-webauthn">{String(useWebAuthn)}</span>
            </div>
          )}
        </MFAVerificationForm>,
        { wrapper: Wrapper }
      )

      await waitFor(() => {
        expect(screen.getByTestId('use-webauthn')).toHaveTextContent('true')
      })
    })
  })

  describe('Error handling', () => {
    it('should call onError when verification fails', async () => {
      const onVerify = vi.fn().mockRejectedValue(new Error('Invalid code'))
      const onError = vi.fn()

      const Wrapper = createWrapper()
      render(
        <MFAVerificationForm onError={onError} onVerify={onVerify}>
          {({ TOTPInput, error }) => (
            <div>
              {TOTPInput}
              {error && <div data-testid="error">{error}</div>}
            </div>
          )}
        </MFAVerificationForm>,
        { wrapper: Wrapper }
      )

      await waitFor(() => {
        expect(screen.getByLabelText('Verification code')).toBeInTheDocument()
      })

      const input = screen.getByLabelText('Verification code')
      fireEvent.change(input, { target: { value: '999999' } })

      await waitFor(() => {
        expect(onError).toHaveBeenCalled()
        expect(screen.getByTestId('error')).toHaveTextContent('Invalid code')
      })
    })

    it('should call onSuccess when verification succeeds', async () => {
      const onVerify = vi.fn().mockResolvedValue(undefined)
      const onSuccess = vi.fn()

      const Wrapper = createWrapper()
      render(
        <MFAVerificationForm onSuccess={onSuccess} onVerify={onVerify}>
          {({ TOTPInput }) => <div>{TOTPInput}</div>}
        </MFAVerificationForm>,
        { wrapper: Wrapper }
      )

      await waitFor(() => {
        expect(screen.getByLabelText('Verification code')).toBeInTheDocument()
      })

      const input = screen.getByLabelText('Verification code')
      fireEvent.change(input, { target: { value: '888888' } })

      await waitFor(() => {
        expect(onSuccess).toHaveBeenCalled()
      })
    })
  })
})
