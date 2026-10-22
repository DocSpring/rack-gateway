import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useEffect } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { StepUpRequest } from '@/contexts/step-up-queue'
import { StepUpProvider, useStepUp } from './step-up-context'

const { client, mockVerifyMFA } = vi.hoisted(() => {
  const interceptors = { use: () => 0, eject: () => undefined }
  return {
    client: { defaults: {}, interceptors: { request: interceptors, response: interceptors } },
    mockVerifyMFA: vi.fn(),
  }
})

vi.mock('@/contexts/auth-context', () => ({
  useAuth: () => ({ user: { has_trusted_device: true } }),
}))
vi.mock('@/contexts/http-client-context', () => ({ useHttpClient: () => ({ client }) }))
vi.mock('@/lib/api', () => ({
  getMFAStatus: vi.fn(),
  verifyMFA: mockVerifyMFA,
  verifyWebAuthnAssertion: vi.fn(),
}))

// The form answers with a code and shows what verifying it threw, as the real form does.
vi.mock('@/components/mfa-verification-form', async () => {
  const { useState } = await import('react')
  return {
    MFAVerificationForm: ({
      onVerify,
    }: {
      onVerify: (params: { method: 'totp'; code: string; trust_device: boolean }) => Promise<void>
    }) => {
      const [error, setError] = useState<string | null>(null)
      return (
        <>
          <button
            onClick={() => {
              onVerify({ method: 'totp', code: '123456', trust_device: false }).then(
                () => setError(null),
                (err: unknown) => setError(String(err))
              )
            }}
            type="button"
          >
            Verify
          </button>
          {error && <p role="alert">{error}</p>}
        </>
      )
    },
  }
})

function Prompt({ requests }: { requests: StepUpRequest[] }) {
  const { openStepUp } = useStepUp()
  useEffect(() => {
    for (const request of requests) {
      openStepUp(request)
    }
  }, [openStepUp, requests])
  return null
}

function renderPrompt(requests: StepUpRequest[]) {
  render(
    <StepUpProvider>
      <Prompt requests={requests} />
    </StepUpProvider>
  )
}

describe('StepUpProvider', () => {
  beforeEach(() => {
    mockVerifyMFA.mockReset()
  })

  // A prompt that isn't retrying a protected request has the gateway check the code before it closes.
  it('checks a standalone prompt with the gateway', async () => {
    const action = vi.fn()
    mockVerifyMFA.mockRejectedValueOnce(new Error('verification failed'))
    renderPrompt([{ verify: true, action }])
    const user = userEvent.setup()

    await user.click(await screen.findByRole('button', { name: 'Verify' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('verification failed')
    expect(action).not.toHaveBeenCalled()

    mockVerifyMFA.mockResolvedValueOnce({})
    await user.click(screen.getByRole('button', { name: 'Verify' }))
    await waitFor(() => expect(action).toHaveBeenCalled())
    expect(mockVerifyMFA).toHaveBeenLastCalledWith({ code: '123456', trust_device: false })
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Verify' })).toBeNull())
  })

  // When the retried request fails for a reason other than MFA, it's settled with that error and the requests
  // waiting behind it still run.
  it('settles a primary request that fails for another reason, and retries the rest', async () => {
    const forbidden = new Error('forbidden')
    const primary = {
      action: () => Promise.reject(forbidden),
      onResolve: vi.fn(),
      onReject: vi.fn(),
    }
    const waiting = { action: () => 'ok', onResolve: vi.fn(), onReject: vi.fn() }
    renderPrompt([primary, waiting])

    await userEvent.setup().click(await screen.findByRole('button', { name: 'Verify' }))

    await waitFor(() => expect(primary.onReject).toHaveBeenCalledWith(forbidden))
    await waitFor(() => expect(waiting.onResolve).toHaveBeenCalledWith('ok'))
    expect(primary.onResolve).not.toHaveBeenCalled()
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Verify' })).toBeNull())
  })
})
