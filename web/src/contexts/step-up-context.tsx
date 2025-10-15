import { type AxiosError, AxiosHeaders, type InternalAxiosRequestConfig, isAxiosError } from 'axios'
import type { ReactNode } from 'react'
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'

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
import { getMFAStatus } from '@/lib/api'
import { getErrorMessage } from '@/lib/error-utils'
import { getMfaRequirementForRequest } from '@/lib/mfa-preflight'

const DEBUG_STEP_UP = false

function debugLog(...args: unknown[]) {
  if (DEBUG_STEP_UP) {
    // biome-ignore lint/suspicious/noConsole: debug logs
    console.log('[STEP_UP]', ...args)
  }
}

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
    const newExpiry = user?.recent_step_up_expires_at
      ? new Date(user.recent_step_up_expires_at)
      : null
    debugLog(
      'useEffect[user.recent_step_up_expires_at] - updating ref from user object:',
      newExpiry?.toISOString(),
      'current ref:',
      stepUpExpiresAtRef.current?.toISOString()
    )

    // Only update if:
    // 1. Current ref is null/undefined AND new value exists (initial sync)
    // 2. New value is NEWER than current ref (user object was refreshed with newer data)
    // NEVER let undefined/null from user object overwrite a valid ref value
    if (!stepUpExpiresAtRef.current && newExpiry) {
      stepUpExpiresAtRef.current = newExpiry
      debugLog(
        'useEffect[user.recent_step_up_expires_at] - updated ref to:',
        newExpiry?.toISOString()
      )
    } else if (newExpiry && stepUpExpiresAtRef.current && newExpiry > stepUpExpiresAtRef.current) {
      stepUpExpiresAtRef.current = newExpiry
      debugLog(
        'useEffect[user.recent_step_up_expires_at] - updated ref to newer value:',
        newExpiry?.toISOString()
      )
    } else {
      debugLog('useEffect[user.recent_step_up_expires_at] - skipping update, keeping current ref')
    }
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
      return
    }
    return await Promise.resolve(action())
  }, [])

  const refreshStepUpExpiry = useCallback(async (): Promise<Date | null> => {
    debugLog('refreshStepUpExpiry called')
    if (!statusFetchRef.current) {
      statusFetchRef.current = getMFAStatus()
        .then((status) => {
          const expires = status.recent_step_up_expires_at
            ? new Date(status.recent_step_up_expires_at)
            : null
          debugLog('refreshStepUpExpiry - fetched status, expires:', expires?.toISOString())
          stepUpExpiresAtRef.current = expires
          return expires
        })
        .finally(() => {
          statusFetchRef.current = null
        })
    }
    return await statusFetchRef.current
  }, [])

  const needsStepUpPrompt = useCallback(async (): Promise<boolean> => {
    const bufferMs = 10_000
    let expires = stepUpExpiresAtRef.current
    debugLog(
      'needsStepUpPrompt - current expires:',
      expires?.toISOString(),
      'now:',
      new Date().toISOString()
    )

    if (!expires || expires.getTime() - Date.now() <= bufferMs) {
      debugLog('needsStepUpPrompt - expires stale or missing, refreshing...')
      expires = await refreshStepUpExpiry()
    }

    const needs = !expires || expires.getTime() - Date.now() <= bufferMs
    debugLog('needsStepUpPrompt - result:', needs, 'expires:', expires?.toISOString())
    return needs
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
    debugLog('handleVerificationSuccess called')
    const request = activeRef.current
    activeRef.current = null
    setIsOpen(false)

    try {
      const result = await runAction(request?.action ?? null)
      request?.onResolve?.(result)
      debugLog('handleVerificationSuccess - action succeeded, refreshing expiry...')
      await refreshStepUpExpiry()
      debugLog('handleVerificationSuccess - expiry refreshed')
    } catch (error) {
      debugLog('handleVerificationSuccess - action failed:', error)
      request?.onReject?.(error)
    } finally {
      clearMFAHeaders()
      processQueue()
    }
  }, [processQueue, refreshStepUpExpiry, runAction])

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
    const resolveRequirement = (config: InternalAxiosRequestConfig) => {
      const baseUrl =
        typeof config.baseURL === 'string'
          ? config.baseURL
          : (client.defaults.baseURL as string | undefined)
      return getMfaRequirementForRequest(config.method?.toUpperCase(), config.url, baseUrl)
    }

    const shouldPromptForStepUp = async (mfaLevel: string): Promise<boolean> => {
      if (mfaLevel === 'always') {
        return true
      }
      if (mfaLevel === 'step_up') {
        return await needsStepUpPrompt()
      }
      return false
    }

    const applyMfaRequirement = async (
      internal: InternalRequestConfig,
      requirement: NonNullable<ReturnType<typeof getMfaRequirementForRequest>>
    ): Promise<InternalAxiosRequestConfig> => {
      if (!internal.headers) {
        internal.headers = AxiosHeaders.from({})
      }
      if (internal.__skipMfaInterceptor) {
        internal.__skipMfaInterceptor = undefined
        return internal
      }

      debugLog('applyMfaRequirement', {
        method: internal.method,
        url: internal.url,
        mfaLevel: requirement.mfaLevel,
      })

      const needsPrompt = await shouldPromptForStepUp(requirement.mfaLevel)
      if (needsPrompt) {
        return promptForMfaConfig(internal)
      }

      return internal
    }

    const requestInterceptor = client.interceptors.request.use((config) => {
      const requirement = resolveRequirement(config)
      if (!requirement) {
        return config
      }
      return applyMfaRequirement(config as InternalRequestConfig, requirement)
    })

    const responseInterceptor = client.interceptors.response.use(
      (response) => response,
      (error) => {
        type AxiosStepUpError = AxiosError<unknown, InternalRequestConfig> & {
          suppressToast?: boolean
        }
        const axiosError = error as AxiosStepUpError
        if (!isMFAError(axiosError)) {
          return Promise.reject(axiosError)
        }

        const originalConfig = (axiosError.config ?? {}) as InternalRequestConfig

        if (originalConfig.__skipMfaInterceptor) {
          return Promise.reject(axiosError)
        }

        if (axiosError.config) {
          ;(axiosError.config as InternalRequestConfig).__suppressGlobalError = true
        }
        axiosError.suppressToast = true

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
          <DialogHeader className="text-center sm:text-center">
            <DialogTitle className="text-center">Multi-Factor Authentication Required</DialogTitle>
          </DialogHeader>

          <DialogDescription className="text-center" />
          <MFAVerificationForm
            mode="step-up"
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
            renderCancelButton={() => (
              <Button onClick={closeStepUp} type="button" variant="outline">
                Cancel
              </Button>
            )}
            showTrustDevice={!user?.has_trusted_device}
          />
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
