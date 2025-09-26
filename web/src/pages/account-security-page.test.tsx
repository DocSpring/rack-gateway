import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AccountSecurityPage } from './account-security-page'

vi.mock('qrcode', () => {
  const toDataURL = vi.fn().mockResolvedValue('data:image/png;base64,placeholder')
  return {
    default: { toDataURL },
    toDataURL,
  }
})

vi.mock('@tanstack/react-router', () => ({
  Link: ({ to, children, ...props }: { to?: unknown; children?: ReactNode }) => (
    <a href={typeof to === 'string' ? to : '#'} {...props}>
      {children}
    </a>
  ),
  useNavigate: () => vi.fn(),
  useLocation: () => ({
    pathname: '/account/security',
    search: '',
    hash: '',
    params: {},
  }),
}))

vi.mock('@/contexts/auth-context', () => ({
  useAuth: () => ({
    refresh: vi.fn().mockResolvedValue(null),
  }),
}))

vi.mock('@/components/ui/use-toast', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
    warning: vi.fn(),
    info: vi.fn(),
  },
}))

const { apiMocks } = vi.hoisted(() => ({
  apiMocks: {
    getMFAStatus: vi.fn(),
    startTOTPEnrollment: vi.fn(),
    confirmTOTPEnrollment: vi.fn(),
    deleteMFAMethod: vi.fn(),
    revokeTrustedDevice: vi.fn(),
    regenerateBackupCodes: vi.fn(),
    verifyMFA: vi.fn(),
  } satisfies Record<string, (...args: any[]) => unknown>,
}))

vi.mock('@/lib/api', () => ({
  ...apiMocks,
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

describe('AccountSecurityPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.getMFAStatus.mockResolvedValue({
      enrolled: false,
      required: false,
      methods: [],
      trusted_devices: [],
      backup_codes: { total: 10, unused: 10 },
    })
  })

  it('renders MFA status', async () => {
    const Wrapper = createWrapper()
    render(<AccountSecurityPage />, { wrapper: Wrapper })

    await waitFor(() => {
      expect(screen.getByText('Account Security')).toBeInTheDocument()
      expect(screen.getByText('Multi-Factor Authentication')).toBeInTheDocument()
    })

    expect(apiMocks.getMFAStatus).toHaveBeenCalled()
  })

  it('starts enrollment when Enable MFA is clicked', async () => {
    apiMocks.startTOTPEnrollment.mockResolvedValue({
      method_id: 1,
      secret: 'ABC123',
      uri: 'otpauth://totp/Example',
      backup_codes: ['CODE1', 'CODE2'],
    })

    const Wrapper = createWrapper()
    render(<AccountSecurityPage />, { wrapper: Wrapper })

    const enableButton = await screen.findByRole('button', { name: /enable mfa/i })
    await userEvent.click(enableButton)

    await waitFor(() => {
      expect(apiMocks.startTOTPEnrollment).toHaveBeenCalled()
      expect(screen.getByText(/finish mfa enrollment/i)).toBeInTheDocument()
    })
  })
})
