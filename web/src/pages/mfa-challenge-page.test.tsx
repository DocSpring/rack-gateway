import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { MFAChallengePage } from './mfa-challenge-page'

const { verifyCliMfa } = vi.hoisted(() => ({ verifyCliMfa: vi.fn() }))

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return { ...actual, verifyCliMfa }
})

vi.mock('../components/mfa-verification-form', () => ({
  MFAVerificationForm: ({
    onVerify,
  }: {
    onVerify: (params: { method: 'totp'; code: string; trust_device: boolean }) => Promise<void>
  }) => (
    <button
      onClick={() => {
        // Errors are surfaced by the page itself; the stub only swallows the rejection.
        onVerify({ method: 'totp', code: '123456', trust_device: false }).catch(
          (error: unknown) => error
        )
      }}
      type="button"
    >
      Verify
    </button>
  ),
}))

const assign = vi.fn()

function renderPage(search: string, children?: ReactNode) {
  vi.stubGlobal('location', { ...window.location, assign, search })
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <MFAChallengePage />
      {children}
    </QueryClientProvider>
  )
}

describe('MFAChallengePage CLI login', () => {
  beforeEach(() => {
    assign.mockReset()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('shows where the CLI login was started', () => {
    renderPage('?state=gateway-state&device=laptop.local&ip=203.0.113.7')
    expect(screen.getByTestId('cli-login-initiator')).toHaveTextContent(
      'Login started from laptop.local, IP 203.0.113.7'
    )
  })

  it('hands the browser back to the CLI after MFA', async () => {
    verifyCliMfa.mockResolvedValue({ redirect: '/api/v1/auth/cli/return?state=gateway-state' })
    renderPage('?state=gateway-state')

    await userEvent.click(screen.getByRole('button', { name: 'Verify' }))

    await waitFor(() =>
      expect(assign).toHaveBeenCalledWith('/api/v1/auth/cli/return?state=gateway-state')
    )
    expect(verifyCliMfa).toHaveBeenCalledWith({ state: 'gateway-state', code: '123456' })
  })

  it('shows an error instead of guessing the next step', async () => {
    verifyCliMfa.mockResolvedValue({ redirect: '' })
    renderPage('?state=gateway-state')

    await userEvent.click(screen.getByRole('button', { name: 'Verify' }))

    expect(await screen.findByText(/did not return the next login step/i)).toBeInTheDocument()
    expect(assign).not.toHaveBeenCalled()
  })
})
