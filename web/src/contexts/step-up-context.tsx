import { AxiosHeaders, isAxiosError, type InternalAxiosRequestConfig } from 'axios'
import type { ReactNode } from 'react'
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'

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
import { useHttpClient } from '@/contexts/http-client-context'
import { getErrorMessage } from '@/lib/error-utils'
import { getMFAStatus } from '@/lib/api'
import { getMfaRequirementForRequest } from '@/lib/mfa-preflight'

type StepUpAction = (() => Promise<unknown>) | (() => unknown) | null

type StepUpRequest = {
  action?: StepUpAction
  onResolve?: (value: unknown) => void
  onReject?: (error: unknown) => void
}

type StepUpContextValue = {
  openStepUp: (request?: StepUpRequest) => void
  requireStepUp: (action: NonNullable<StepUpAction>) => void
  handleStepUpError: (error: unknown, action: NonNullable<StepUpAction>) => boolean
  runWithMFAGuard<T>(fn: () => Promise<T>): Promise<T>
  closeStepUp: () => void
  isOpen: boolean
  isVerifying: boolean
}

const StepUpContext = createContext<StepUpContextValue | undefined>(undefined)

type MfaHeaders = { 'X-MFA-TOTP'?: string; 'X-MFA-WebAuthn'?: string }

let currentMFAHeaders: MfaHeaders = {}

export function getMFAHeaders(): MfaHeaders {
  return currentMFAHeaders
}

export function clearMFAHeaders(): void {
  currentMFAHeaders = {}
}

export function isMFAError(error: unknown): boolean {
  if (!isAxiosError(error)) {
    return false
  }
  if (error.response?.status !== 401) {
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

type InternalRequestConfig = InternalAxiosRequestConfig & {
  __skipMfaInterceptor?: boolean
  __suppressGlobalError?: boolean
}

export function StepUpProvider({ children }: { children: ReactNode }): React.ReactElement {
  const { user } = useAuth()
  const { client } = useHttpClient()
  const [isOpen, setIsOpen] = useState(false)
  const [isVerifying, setIsVerifying] = useState(false)

  const queueRef = useRef<StepUpRequest[]>([])
  const activeRef = useRef<StepUpRequest | null>(null)
  const stepUpExpiresAtRef = useRef<Date | null>(
    user?.recent_step_up_expires_at ? new Date(user.recent_step_up_expires_at) : null
  )
  const statusFetchRef = useRef<Promise<Date | null> | null>(null)

  useEffect(() => {
    stepUpExpiresAtRef.current = user?.recent_step_up_expires_at
      ? new Date(user.recent_step_up_expires_at)
      : null
  }, [user?.recent_step_up_expires_at])

  const processQueue = useCallback(() => {
    if (activeRef.current || queueRef.current.length === 0) {
      return
    }
    const next = queueRef.current.shift() ?? null
    activeRef.current = next
    if (next) {
      setIsOpen(true)
    }
  }, [])

  const closeStepUp = useCallback(() => {
    if (isVerifying) {
      return
    }
    setIsOpen(false)
    if (activeRef.current?.onReject) {
      activeRef.current.onReject(new Error('MFA verification cancelled'))
    }
    activeRef.current = null
    queueRef.current = []
  }, [isVerifying])

  const openStepUp = useCallback(
    (request?: StepUpRequest) => {
      queueRef.current.push(request ?? {})
      processQueue()
    },
    [processQueue]
  )

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

  const runAction = useCallback(async (action: StepUpAction) => {
    if (!action) {
      return undefined
    }
    return await Promise.resolve(action())
  }, [])

  const refreshStepUpExpiry = useCallback(async (): Promise<Date | null> => {
    if (!statusFetchRef.current) {
      statusFetchRef.current = getMFAStatus()
        .then((status) => {
          const expires = status.recent_step_up_expires_at
            ? new Date(status.recent_step_up_expires_at)
            : null
          stepUpExpiresAtRef.current = expires
          return expires
        })
        .finally(() => {
          statusFetchRef.current = null
        })
    }
    return statusFetchRef.current
  }, [])

  const needsStepUpPrompt = useCallback(async (): Promise<boolean> => {
    const bufferMs = 10_000
    let expires = stepUpExpiresAtRef.current
    if (!expires || expires.getTime() - Date.now() <= bufferMs) {
      expires = await refreshStepUpExpiry()
    }
    return !expires || expires.getTime() - Date.now() <= bufferMs
  }, [refreshStepUpExpiry])

  const promptForMfaConfig = useCallback(
    (originalConfig: InternalRequestConfig): Promise<InternalAxiosRequestConfig> =>
      new Promise((resolve, reject) => {
        openStepUp({
          action: () => {
            const headers = AxiosHeaders.from(originalConfig.headers ?? {})
            const mfaHeaders = getMFAHeaders()
            for (const [key, value] of Object.entries(mfaHeaders)) {
              if (value) {
                headers.set(key, value)
              } else {
                headers.delete(key)
              }
            }

            const nextConfig: InternalRequestConfig = {
              ...originalConfig,
              headers,
              __skipMfaInterceptor: true,
            }

            return nextConfig
          },
          onResolve: (value) => resolve(value as InternalAxiosRequestConfig),
          onReject: reject,
        })
      }),
    [openStepUp]
  )

  const handleVerificationSuccess = useCallback(async () => {
    const request = activeRef.current
    activeRef.current = null
    setIsOpen(false)

    try {
      const result = await runAction(request?.action ?? null)
      request?.onResolve?.(result)
      await refreshStepUpExpiry()
    } catch (error) {
      request?.onReject?.(error)
    } finally {
      clearMFAHeaders()
      processQueue()
    }
  }, [processQueue, runAction])

  const runWithMFAGuard = useCallback(
    async <T,>(fn: () => Promise<T>): Promise<T> => {
      try {
        return await fn()
      } catch (error) {
        if (!isMFAError(error)) {
          throw error
        }

        return await new Promise<T>((resolve, reject) => {
          openStepUp({
            action: () => fn(),
            onResolve: (value) => resolve(value as T),
            onReject: reject,
          })
        })
      }
    },
    [openStepUp]
  )

  useEffect(() => {
    const requestInterceptor = client.interceptors.request.use(async (config) => {
      const baseUrl =
        typeof config.baseURL === 'string'
          ? config.baseURL
          : (client.defaults.baseURL as string | undefined)
      const requirement = getMfaRequirementForRequest(
        config.method?.toUpperCase(),
        config.url,
        baseUrl
      )

      if (!requirement) {
        return config
      }

      const internal = config as InternalRequestConfig
      if (internal.__skipMfaInterceptor) {
        delete internal.__skipMfaInterceptor
        return config
      }

      if (requirement.mfaLevel === 'always') {
        return promptForMfaConfig(internal)
      }

      if (requirement.mfaLevel === 'step_up') {
        const prompt = await needsStepUpPrompt()
        if (!prompt) {
          return config
        }
        return promptForMfaConfig(internal)
      }

      return config
    })

    const responseInterceptor = client.interceptors.response.use(
      (response) => response,
      (error) => {
        if (!isMFAError(error)) {
          return Promise.reject(error)
        }

        const originalConfig: InternalRequestConfig = {
          ...(error.config ?? {}),
        }

        if (originalConfig.__skipMfaInterceptor) {
          return Promise.reject(error)
        }

        if (error?.config) {
          ;(error.config as InternalRequestConfig).__suppressGlobalError = true
        }
        ;(error as any).suppressToast = true

        return new Promise((resolve, reject) => {
          openStepUp({
            action: async () => {
              const headers = AxiosHeaders.from(originalConfig.headers ?? {})
              const mfaHeaders = getMFAHeaders()
              for (const [key, value] of Object.entries(mfaHeaders)) {
                if (value) {
                  headers.set(key, value)
                } else {
                  headers.delete(key)
                }
              }

              const retryConfig: InternalRequestConfig = {
                ...originalConfig,
                headers,
                __skipMfaInterceptor: true,
              }

              const response = await client.request(retryConfig)
              return response
            },
            onResolve: resolve,
            onReject: reject,
          })
        })
      }
    )

    return () => {
      client.interceptors.request.eject(requestInterceptor)
      client.interceptors.response.eject(responseInterceptor)
    }
  }, [client, needsStepUpPrompt, openStepUp, promptForMfaConfig])

  const contextValue = useMemo<StepUpContextValue>(
    () => ({
      openStepUp,
      requireStepUp,
      handleStepUpError,
      runWithMFAGuard,
      closeStepUp,
      isOpen,
      isVerifying,
    }),
    [
      closeStepUp,
      handleStepUpError,
      isOpen,
      isVerifying,
      openStepUp,
      requireStepUp,
      runWithMFAGuard,
    ]
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
                clearMFAHeaders()
                if (params.method === 'totp') {
                  currentMFAHeaders['X-MFA-TOTP'] = params.code
                } else {
                  const webauthnData = JSON.stringify({
                    session_data: params.session_data,
                    assertion_response: params.assertion_response,
                  })
                  currentMFAHeaders['X-MFA-WebAuthn'] = btoa(webauthnData)
                }

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
                  <Button onClick={closeStepUp} type="button" variant="outline">
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

export function useStepUp(): StepUpContextValue {
  const context = useContext(StepUpContext)
  if (!context) {
    throw new Error('useStepUp must be used within a StepUpProvider')
  }
  return context
}
