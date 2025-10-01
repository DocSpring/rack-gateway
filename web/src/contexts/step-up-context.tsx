import { isAxiosError } from 'axios'
import type { ReactNode } from 'react'
import { createContext, useCallback, useContext, useMemo, useState } from 'react'
import { MFAInput } from '@/components/mfa-input'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { toast } from '@/components/ui/use-toast'
import { useAuth } from '@/contexts/auth-context'
import { verifyMFA } from '@/lib/api'
import { getErrorMessage } from '@/lib/error-utils'

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

function isAxiosStepUpError(error: unknown): boolean {
  if (!isAxiosError(error)) {
    return false
  }
  const status = error.response?.status
  if (status !== 401) {
    return false
  }
  const errorCode = (error.response?.data as { error?: string } | undefined)?.error
  const header = error.response?.headers?.['x-mfa-required']
  return errorCode === 'mfa_step_up_required' || header === 'step-up'
}

export function StepUpProvider({ children }: { children: ReactNode }) {
  const { refresh, user } = useAuth()
  const [isOpen, setIsOpen] = useState(false)
  const [verificationCode, setVerificationCode] = useState('')
  const [trustDevice, setTrustDevice] = useState(true)
  const [isVerifying, setIsVerifying] = useState(false)
  const [pendingAction, setPendingAction] = useState<StepUpAction>(null)

  const resetForm = useCallback(() => {
    setVerificationCode('')
    setTrustDevice(true)
  }, [])

  const closeStepUp = useCallback(() => {
    if (isVerifying) {
      return
    }
    setIsOpen(false)
    resetForm()
    setPendingAction(null)
  }, [isVerifying, resetForm])

  const openStepUp = useCallback(
    (request?: StepUpRequest) => {
      setPendingAction(() => request?.action ?? null)
      resetForm()
      setIsOpen(true)
    },
    [resetForm]
  )

  const requireStepUp = useCallback(
    (action: NonNullable<StepUpAction>) => {
      openStepUp({ action })
    },
    [openStepUp]
  )

  const handleStepUpError = useCallback(
    (error: unknown, action: NonNullable<StepUpAction>) => {
      if (!isAxiosStepUpError(error)) {
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
        if (isAxiosStepUpError(error)) {
          openStepUp({ action })
          return
        }
        const message = getErrorMessage(error, 'Action failed after MFA verification')
        toast.error(message)
      }
    },
    [openStepUp]
  )

  const handleVerify = useCallback(async () => {
    if (!verificationCode) {
      toast.error('Enter a verification code to continue')
      return
    }
    setIsVerifying(true)
    try {
      await verifyMFA({ code: verificationCode, trust_device: trustDevice })
      toast.success('MFA verification successful')
      const action = pendingAction
      setPendingAction(null)
      setIsOpen(false)
      resetForm()
      try {
        await refresh()
      } catch {
        /* ignore user refresh errors */
      }
      await runPendingAction(action)
    } catch (error) {
      const message = getErrorMessage(error, 'Verification failed')
      toast.error(message)
    } finally {
      setIsVerifying(false)
    }
  }, [pendingAction, refresh, resetForm, runPendingAction, trustDevice, verificationCode])

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
              Enter a code from your authenticator or an unused backup code to continue.
            </DialogDescription>
          </DialogHeader>
          <form
            className="space-y-4"
            onSubmit={(event) => {
              event.preventDefault()
              handleVerify().catch(() => {
                /* errors handled in handler */
              })
            }}
          >
            <div className="space-y-2">
              <Label htmlFor="step-up-code">Verification code</Label>
              <MFAInput
                autoFocus
                id="step-up-code"
                maxLength={6}
                onChange={(event) => setVerificationCode(event.target.value.trim())}
                placeholder="123456"
                required
                value={verificationCode}
              />
            </div>
            {!user?.has_trusted_device && (
              <label className="flex items-center gap-2 text-sm">
                <input
                  checked={trustDevice}
                  onChange={(event) => setTrustDevice(event.target.checked)}
                  type="checkbox"
                />
                Trust this browser for 30 days
              </label>
            )}
            <div className="flex justify-end gap-2">
              <Button
                onClick={() => {
                  closeStepUp()
                }}
                type="button"
                variant="outline"
              >
                Cancel
              </Button>
              <Button disabled={isVerifying || verificationCode.length === 0} type="submit">
                {isVerifying ? 'Verifying…' : 'Verify'}
              </Button>
            </div>
          </form>
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

export function isStepUpError(error: unknown): boolean {
  return isAxiosStepUpError(error)
}
