import { isAxiosError } from 'axios'
import type { ReactNode } from 'react'
import { createContext, useCallback, useContext, useMemo, useState } from 'react'
import { LoadingSpinner } from '@/components/loading-spinner'
import { MFAVerificationForm } from '@/components/mfa-verification-form'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { toast } from '@/components/ui/use-toast'
import { useAuth } from '@/contexts/auth-context'
import { getErrorMessage } from '@/lib/error-utils'

// Store the MFA code/assertion in a closure that the action can access
let currentMFAHeaders: { 'X-MFA-TOTP'?: string; 'X-MFA-WebAuthn'?: string } = {}

export function getMFAHeaders() {
  return currentMFAHeaders
}

export function clearMFAHeaders() {
  currentMFAHeaders = {}
}

type StepUpAction = (() => Promise<void>) | (() => void) | null

type StepUpRequest = {
  action?: StepUpAction
}

type StepUpContextValue = {
  openStepUp: (request?: StepUpRequest) => void
  requireStepUp: (action: NonNullable<StepUpAction>) => void
  handleStepUpError: (error: unknown, action: NonNullable<StepUpAction>) => boolean
  closeStepUp: () => void
  isOpen: boolean
  isVerifying: boolean
}

const StepUpContext = createContext<StepUpContextValue | undefined>(undefined)

export function isMFAError(error: unknown): boolean {
  if (!isAxiosError(error)) {
    return false
  }
  const status = error.response?.status
  if (status !== 401) {
    return false
  }
  const errorCode = (error.response?.data as { error?: string } | undefined)?.error
  const header = error.response?.headers?.['x-mfa-required']
  return (
    errorCode === 'mfa_step_up_required' ||
    errorCode === 'mfa_required' ||
    header === 'step-up' ||
    header === 'always'
  )
}

export function StepUpProvider({ children }: { children: ReactNode }) {
  const { user } = useAuth()
  const [isOpen, setIsOpen] = useState(false)
  const [isVerifying, setIsVerifying] = useState(false)
  const [pendingAction, setPendingAction] = useState<StepUpAction>(null)

  const closeStepUp = useCallback(() => {
    if (isVerifying) {
      return
    }
    setIsOpen(false)
    setPendingAction(null)
  }, [isVerifying])

  const openStepUp = useCallback((request?: StepUpRequest) => {
    setPendingAction(() => request?.action ?? null)
    setIsOpen(true)
  }, [])

  const requireStepUp = useCallback(
    (action: NonNullable<StepUpAction>) => {
      openStepUp({ action })
    },
    [openStepUp]
  )

  const handleStepUpError = useCallback(
    (error: unknown, action: NonNullable<StepUpAction>) => {
      if (!isMFAError(error)) {
        return false
      }
      openStepUp({ action })
      return true
    },
    [openStepUp]
  )

  const runPendingAction = useCallback(
    async (action: StepUpAction) => {
      if (!action) {
        return
      }
      try {
        await Promise.resolve(action())
      } catch (error) {
        if (isMFAError(error)) {
          openStepUp({ action })
          return
        }
        const message = getErrorMessage(error, 'Action failed after MFA verification')
        toast.error(message)
      }
    },
    [openStepUp]
  )

  const handleVerificationSuccess = useCallback(async () => {
    const action = pendingAction
    setPendingAction(null)
    setIsOpen(false)

    // Don't refresh - the MFA code is in headers and will be sent with the retry
    await runPendingAction(action)

    // Clear MFA headers after action completes
    clearMFAHeaders()
  }, [pendingAction, runPendingAction])

  const contextValue = useMemo<StepUpContextValue>(
    () => ({
      openStepUp,
      requireStepUp,
      handleStepUpError,
      closeStepUp,
      isOpen,
      isVerifying,
    }),
    [closeStepUp, handleStepUpError, isOpen, isVerifying, openStepUp, requireStepUp]
  )

  return (
    <StepUpContext.Provider value={contextValue}>
      {children}
      <Dialog
        onOpenChange={(open) => {
          if (open) {
            setIsOpen(true)
          } else {
            closeStepUp()
          }
        }}
        open={isOpen}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Multi-Factor Authentication Required</DialogTitle>
            <DialogDescription>
              Verify your identity to continue with this sensitive action.
            </DialogDescription>
          </DialogHeader>

          <MFAVerificationForm
            onError={(error) => {
              toast.error(getErrorMessage(error, 'Verification failed'))
            }}
            onVerify={async (params) => {
              setIsVerifying(true)
              try {
                // Store MFA credentials in headers object for the retry
                clearMFAHeaders()
                if (params.method === 'totp') {
                  currentMFAHeaders['X-MFA-TOTP'] = params.code
                } else {
                  // For WebAuthn, encode the session_data and assertion_response as base64 JSON
                  const webauthnData = JSON.stringify({
                    session_data: params.session_data,
                    assertion_response: params.assertion_response,
                  })
                  currentMFAHeaders['X-MFA-WebAuthn'] = btoa(webauthnData)
                }

                // Retry the action with MFA headers set
                await handleVerificationSuccess()
              } finally {
                setIsVerifying(false)
              }
            }}
            showTrustDevice={!user?.has_trusted_device}
          >
            {({
              TOTPInput,
              TrustDeviceCheckbox,
              MethodSwitchButtons,
              useWebAuthn,
              isVerifying: formVerifying,
              handleVerifyWebAuthn,
            }) => (
              <div className="space-y-4">
                {useWebAuthn ? (
                  <>
                    <Button
                      className="w-full"
                      disabled={formVerifying}
                      onClick={() => {
                        handleVerifyWebAuthn().catch(() => {
                          /* errors handled by onError */
                        })
                      }}
                    >
                      {formVerifying ? (
                        <LoadingSpinner className="size-4" variant="white" />
                      ) : (
                        'Authenticate with Security Key'
                      )}
                    </Button>
                    {TrustDeviceCheckbox}
                    {MethodSwitchButtons}
                  </>
                ) : (
                  <>
                    {TOTPInput}
                    {TrustDeviceCheckbox}
                    {MethodSwitchButtons}
                  </>
                )}
                <div className="flex justify-end">
                  <Button
                    onClick={() => {
                      closeStepUp()
                    }}
                    type="button"
                    variant="outline"
                  >
                    Cancel
                  </Button>
                </div>
              </div>
            )}
          </MFAVerificationForm>
        </DialogContent>
      </Dialog>
    </StepUpContext.Provider>
  )
}

export function useStepUp() {
  const context = useContext(StepUpContext)
  if (!context) {
    throw new Error('useStepUp must be used within a StepUpProvider')
  }
  return context
}
