import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { UserMFAResetDialog } from './user-mfa-reset-dialog'

vi.mock('@/lib/api', () => ({
  api: {
    resetUserMFA: vi.fn(),
  },
}))

const RESET_BUTTON = /^reset mfa$/i

function renderDialog(onOpenChange = vi.fn()) {
  const queryClient = new QueryClient({ defaultOptions: { mutations: { retry: false } } })
  render(
    <QueryClientProvider client={queryClient}>
      <UserMFAResetDialog onOpenChange={onOpenChange} open={true} userEmail="dev@example.com" />
    </QueryClientProvider>
  )
  return onOpenChange
}

describe('UserMFAResetDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('says what a reset removes and keeps', () => {
    renderDialog()
    expect(screen.getByText(/backup codes and trusted devices/)).toBeInTheDocument()
    expect(screen.getByText(/API tokens are kept/)).toBeInTheDocument()
  })

  it('resets the user and closes', async () => {
    vi.mocked(api.resetUserMFA).mockResolvedValue({ status: 'reset' })
    const onOpenChange = renderDialog()

    fireEvent.click(screen.getByRole('button', { name: RESET_BUTTON }))

    await waitFor(() => {
      expect(api.resetUserMFA).toHaveBeenCalledWith('dev@example.com')
      expect(onOpenChange).toHaveBeenCalledWith(false)
    })
  })

  it('stays open when the reset fails', async () => {
    vi.mocked(api.resetUserMFA).mockRejectedValue(new Error('failed to reset MFA'))
    const onOpenChange = renderDialog()

    fireEvent.click(screen.getByRole('button', { name: RESET_BUTTON }))

    await waitFor(() => expect(api.resetUserMFA).toHaveBeenCalled())
    expect(onOpenChange).not.toHaveBeenCalled()
  })
})
