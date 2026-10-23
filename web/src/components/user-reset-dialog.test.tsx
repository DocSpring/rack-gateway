import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { type UserReset, UserResetDialog } from './user-reset-dialog'

vi.mock('@/lib/api', () => ({
  api: {
    resetUserMFA: vi.fn(),
    resetUserSignIn: vi.fn(),
  },
}))

const RESET_MFA_BUTTON = /^reset mfa$/i
const RESET_SIGN_IN_BUTTON = /^reset sign-in$/i

function renderDialog(reset: UserReset, onOpenChange = vi.fn()) {
  const queryClient = new QueryClient({ defaultOptions: { mutations: { retry: false } } })
  render(
    <QueryClientProvider client={queryClient}>
      <UserResetDialog
        onOpenChange={onOpenChange}
        open={true}
        reset={reset}
        userEmail="dev@example.com"
      />
    </QueryClientProvider>
  )
  return onOpenChange
}

describe('UserResetDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('says what an MFA reset removes and keeps', () => {
    renderDialog('mfa')
    expect(screen.getByText(/backup codes and trusted devices/)).toBeInTheDocument()
    expect(screen.getByText(/API tokens are kept/)).toBeInTheDocument()
  })

  it('resets the MFA of the user and closes', async () => {
    vi.mocked(api.resetUserMFA).mockResolvedValue({ status: 'reset' })
    const onOpenChange = renderDialog('mfa')

    fireEvent.click(screen.getByRole('button', { name: RESET_MFA_BUTTON }))

    await waitFor(() => {
      expect(api.resetUserMFA).toHaveBeenCalledWith('dev@example.com')
      expect(onOpenChange).toHaveBeenCalledWith(false)
    })
  })

  it('stays open when the reset fails', async () => {
    vi.mocked(api.resetUserMFA).mockRejectedValue(new Error('failed to reset MFA'))
    const onOpenChange = renderDialog('mfa')

    fireEvent.click(screen.getByRole('button', { name: RESET_MFA_BUTTON }))

    await waitFor(() => expect(api.resetUserMFA).toHaveBeenCalled())
    expect(onOpenChange).not.toHaveBeenCalled()
  })

  it('resets the sign-in of the user, keeping their MFA', async () => {
    vi.mocked(api.resetUserSignIn).mockResolvedValue({ status: 'reset' })
    const onOpenChange = renderDialog('sign-in')
    expect(screen.getByText(/MFA methods and API tokens are kept/)).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: RESET_SIGN_IN_BUTTON }))

    await waitFor(() => {
      expect(api.resetUserSignIn).toHaveBeenCalledWith('dev@example.com')
      expect(onOpenChange).toHaveBeenCalledWith(false)
    })
    expect(api.resetUserMFA).not.toHaveBeenCalled()
  })
})
