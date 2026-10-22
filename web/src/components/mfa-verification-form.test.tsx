import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { MFAVerificationForm } from './mfa-verification-form'

const { mockGetMFAStatus } = vi.hoisted(() => ({ mockGetMFAStatus: vi.fn() }))

vi.mock('@/lib/api', () => ({
  getMFAStatus: mockGetMFAStatus,
  startWebAuthnAssertion: vi.fn(),
}))

function mfaStatus(unusedBackupCodes: number) {
  return {
    enrolled: true,
    required: true,
    methods: [{ id: 1, type: 'totp', label: 'Phone', created_at: '2026-01-01T00:00:00Z' }],
    trusted_devices: [],
    backup_codes: { total: 10, unused: unusedBackupCodes },
    webauthn_available: false,
    trusted_device_ttl_days: 14,
  }
}

// The label shows the gateway's trusted device TTL.
const TRUST_LABEL = 'Trust this device for 14 days'

function renderForm(onVerify = vi.fn(() => Promise.resolve())) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
  render(<MFAVerificationForm onVerify={onVerify} />, { wrapper })
  return onVerify
}

async function waitForMethodsToLoad() {
  // The trust-device checkbox is disabled while the MFA status loads
  await waitFor(() => expect(screen.getByRole('checkbox')).toBeEnabled())
}

describe('MFAVerificationForm backup codes', () => {
  beforeEach(() => {
    mockGetMFAStatus.mockReset()
  })

  it('verifies a normalized backup code without trusting the device', async () => {
    mockGetMFAStatus.mockResolvedValue(mfaStatus(5))
    const onVerify = renderForm()
    const user = userEvent.setup()
    await waitForMethodsToLoad()

    await user.click(screen.getByRole('button', { name: 'Use a backup code' }))
    expect(screen.queryByText(TRUST_LABEL)).not.toBeInTheDocument()

    const verifyButton = screen.getByRole('button', { name: 'Verify backup code' })
    await user.type(screen.getByLabelText('Backup code'), 'abcdef-01234')
    expect(verifyButton).toBeDisabled()

    await user.type(screen.getByLabelText('Backup code'), '5')
    await user.click(verifyButton)

    await waitFor(() =>
      expect(onVerify).toHaveBeenCalledWith({
        method: 'totp',
        code: 'ABCDEF012345',
        trust_device: false,
      })
    )
  })

  it('switches back to the authenticator app', async () => {
    mockGetMFAStatus.mockResolvedValue(mfaStatus(5))
    renderForm()
    const user = userEvent.setup()
    await waitForMethodsToLoad()

    await user.click(screen.getByRole('button', { name: 'Use a backup code' }))
    await user.click(screen.getByRole('button', { name: 'Back to authenticator app' }))

    expect(screen.queryByLabelText('Backup code')).not.toBeInTheDocument()
    expect(screen.getByText(TRUST_LABEL)).toBeInTheDocument()
  })

  it('hides the option when no backup codes are left', async () => {
    mockGetMFAStatus.mockResolvedValue(mfaStatus(0))
    renderForm()
    await waitForMethodsToLoad()

    expect(screen.queryByRole('button', { name: 'Use a backup code' })).not.toBeInTheDocument()
  })
})

describe('MFAVerificationForm errors', () => {
  it('shows why a code was refused', async () => {
    mockGetMFAStatus.mockResolvedValue(mfaStatus(5))
    renderForm(vi.fn(() => Promise.reject(new Error('Account locked: too many failed attempts'))))
    const user = userEvent.setup()
    await waitForMethodsToLoad()

    await user.click(screen.getByRole('button', { name: 'Use a backup code' }))
    await user.type(screen.getByLabelText('Backup code'), 'abcdef-012345')
    await user.click(screen.getByRole('button', { name: 'Verify backup code' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Account locked: too many failed attempts'
    )
  })
})

describe('MFAVerificationForm YubiKey OTP', () => {
  const otp = 'ccccccbcgujhingjrdejhgfnuetrgigvejhhgbkugded'

  it('takes the OTP from a user whose only factor is a YubiKey', async () => {
    mockGetMFAStatus.mockResolvedValue({
      ...mfaStatus(0),
      methods: [{ id: 2, type: 'yubiotp', label: 'Yubikey', created_at: '2026-01-01T00:00:00Z' }],
    })
    const onVerify = renderForm()
    const user = userEvent.setup()
    await waitForMethodsToLoad()

    await user.type(screen.getByLabelText('YubiKey OTP'), `${otp}{Enter}`)

    await waitFor(() =>
      expect(onVerify).toHaveBeenCalledWith({ method: 'totp', code: otp, trust_device: true })
    )
  })

  it('offers it alongside an authenticator app', async () => {
    const status = mfaStatus(0)
    mockGetMFAStatus.mockResolvedValue({
      ...status,
      methods: [
        ...status.methods,
        { id: 2, type: 'yubiotp', label: 'Yubikey', created_at: '2026-01-01T00:00:00Z' },
      ],
    })
    renderForm()
    const user = userEvent.setup()
    await waitForMethodsToLoad()

    await user.click(screen.getByRole('button', { name: 'Use a YubiKey OTP' }))
    expect(screen.getByLabelText('YubiKey OTP')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Back to authenticator app' }))
    expect(screen.queryByLabelText('YubiKey OTP')).not.toBeInTheDocument()
  })
})
