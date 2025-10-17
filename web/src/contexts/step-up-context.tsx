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
import { useAuth } from '@/contexts/auth-context'
import { useHttpClient } from '@/contexts/http-client-context'
import { getMFAStatus } from '@/lib/api'
import { getMfaRequirementForRequest } from '@/lib/mfa-preflight'

// Change to true to debug step-up dialog flow
const DEBUG_STEP_UP = false
function debugLog(...args: unknown[]) {
  if (!DEBUG_STEP_UP) {
    return
  }
  // biome-ignore lint/suspicious/noConsole: debug logs
  console.log('[STEP_UP]', ...args)
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
    errorCode === 'mfa_verification_failed' ||
    header === 'step-up' ||
    header === 'always'
  )
}

type InternalRequestConfig = InternalAxiosRequestConfig & {
  __suppressGlobalError?: boolean
  __abortedForMfa?: boolean
  __skipMfaHandling?: boolean
}

export function StepUpProvider({ children }: { children: ReactNode }): React.ReactElement {
  const { user } = useAuth()
  const { client } = useHttpClient()
  const [isOpen, setIsOpen] = useState(false)
  const [isVerifying, setIsVerifying] = useState(false)

  // Single active request
  const activeRequestRef = useRef<StepUpRequest | null>(null)

  const closeStepUp = useCallback(() => {
    if (isVerifying) {
      return
    }
    debugLog('closeStepUp')
    setIsOpen(false)
    if (activeRequestRef.current?.onReject) {
      // Create error with suppressToast flag to prevent duplicate toasts
      const error = new Error('MFA verification cancelled')
      ;(error as Error & { suppressToast?: boolean }).suppressToast = true
      activeRequestRef.current.onReject(error)
    }
    activeRequestRef.current = null
  }, [isVerifying])

  const openStepUp = useCallback((request?: StepUpRequest) => {
    debugLog('openStepUp')
    // Blur any active element (like dropdown menus) to prevent focus conflicts
    if (document.activeElement instanceof HTMLElement) {
      document.activeElement.blur()
    }
    activeRequestRef.current = request ?? {}
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

  const runAction = useCallback(async (action: StepUpAction) => {
    debugLog('runAction invoked', !!action)
    if (!action) {
      return
    }
    return await Promise.resolve(action())
  }, [])

  // This is called by the form when user submits MFA code
  const handleVerify = useCallback(
    async (params: {
      method: 'totp' | 'webauthn'
      code?: string
      trust_device: boolean
      session_data?: string
      assertion_response?: string
    }) => {
      const request = activeRequestRef.current
      if (!request?.action) {
        throw new Error('No active MFA request')
      }

      debugLog('handleVerify called', { method: params.method })

      // Set MFA headers
      clearMFAHeaders()
      if (params.method === 'totp') {
        currentMFAHeaders['X-MFA-TOTP'] = params.code ?? ''
        debugLog('Set TOTP header', { code: params.code })
      } else {
        const webauthnData = JSON.stringify({
          session_data: params.session_data,
          assertion_response: params.assertion_response,
        })
        currentMFAHeaders['X-MFA-WebAuthn'] = btoa(webauthnData)
        debugLog('Set WebAuthn header')
      }

      debugLog('MFA headers before action', getMFAHeaders())

      try {
        // Run the action - this will make a new request with MFA headers
        const result = await runAction(request.action)

        debugLog('handleVerify - action succeeded')

        // Success: close dialog and resolve
        activeRequestRef.current = null
        setIsOpen(false)
        request.onResolve?.(result)
      } catch (error) {
        debugLog('handleVerify - action failed', error)

        // DON'T close dialog, DON'T clear activeRequestRef
        // Just re-throw so the form shows the error inline
        throw error
      } finally {
        clearMFAHeaders()
      }
    },
    [runAction]
  )

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
    // biome-ignore lint/complexity/noExcessiveCognitiveComplexity: very complex logic
    const requestInterceptor = client.interceptors.request.use(async (config) => {
      const internalConfig = config as InternalRequestConfig

      debugLog('Request interceptor checking config', {
        method: internalConfig.method,
        url: internalConfig.url,
        hasSkipFlag: internalConfig.__skipMfaHandling === true,
      })

      // Skip if this request is marked to skip MFA handling
      if (internalConfig.__skipMfaHandling === true) {
        debugLog('Request marked as skip - allowing through')
        return internalConfig
      }

      const baseUrl =
        typeof internalConfig.baseURL === 'string'
          ? internalConfig.baseURL
          : (client.defaults.baseURL as string | undefined)
      const requirement = getMfaRequirementForRequest(
        internalConfig.method?.toUpperCase(),
        internalConfig.url,
        baseUrl
      )

      if (!requirement) {
        return internalConfig
      }

      debugLog('applyMfaRequirement', {
        method: internalConfig.method,
        url: internalConfig.url,
        mfaLevel: requirement.mfaLevel,
      })

      // For 'always': always abort and prompt
      if (requirement.mfaLevel === 'always') {
        debugLog('mfaLevel=always - aborting request to prompt for MFA')
        const controller = new AbortController()
        controller.abort()
        return {
          ...internalConfig,
          signal: controller.signal,
          __abortedForMfa: true,
        }
      }

      // For 'step_up': check status RIGHT NOW
      if (requirement.mfaLevel === 'step_up') {
        debugLog('mfaLevel=step_up - fetching status to check if prompt needed')
        const status = await getMFAStatus()
        const expiresAt = status.recent_step_up_expires_at
          ? new Date(status.recent_step_up_expires_at)
          : null

        const bufferMs = 10_000
        const needsPrompt = !expiresAt || expiresAt.getTime() - Date.now() <= bufferMs

        debugLog('step_up check', {
          expiresAt: expiresAt?.toISOString(),
          needsPrompt,
        })

        if (needsPrompt) {
          debugLog('step_up expired - aborting request to prompt for MFA')
          const controller = new AbortController()
          controller.abort()
          return {
            ...internalConfig,
            signal: controller.signal,
            __abortedForMfa: true,
          }
        }

        debugLog('step_up valid - proceeding with request')
      }

      return internalConfig
    })

    const responseInterceptor = client.interceptors.response.use(
      (response) => response,
      // biome-ignore lint/complexity/noExcessiveCognitiveComplexity: very complex logic
      (error) => {
        type AxiosStepUpError = AxiosError<unknown, InternalRequestConfig> & {
          suppressToast?: boolean
        }
        const axiosError = error as AxiosStepUpError

        // Handle our intentional MFA abort
        if (
          axiosError.code === 'ERR_CANCELED' &&
          (axiosError.config as InternalRequestConfig)?.__abortedForMfa
        ) {
          debugLog('Handling aborted request - opening step-up dialog')

          return new Promise((resolve, reject) => {
            openStepUp({
              // biome-ignore lint/complexity/noExcessiveCognitiveComplexity: very complex logic
              action: async () => {
                const originalConfig = (axiosError.config ?? {}) as InternalRequestConfig
                const mfaHeaders = getMFAHeaders()
                debugLog('Action getting MFA headers', mfaHeaders)

                // Clone headers to avoid mutating original request
                const headers = new AxiosHeaders(originalConfig.headers ?? {})

                // Add MFA headers
                for (const [key, value] of Object.entries(mfaHeaders)) {
                  if (value) {
                    headers.set(key, value)
                  }
                }

                const retryConfig: InternalRequestConfig = {
                  ...originalConfig,
                  headers,
                  signal: undefined, // Remove abort signal
                  __abortedForMfa: undefined,
                  __skipMfaHandling: true,
                }

                debugLog('Making new request with MFA headers', {
                  method: retryConfig.method,
                  url: retryConfig.url,
                  hasXMfaTotp: headers.has('X-MFA-TOTP'),
                  xMfaTotpValue: headers.get('X-MFA-TOTP'),
                  hasXMfaWebAuthn: headers.has('X-MFA-WebAuthn'),
                  xMfaWebAuthnLength: String(headers.get('X-MFA-WebAuthn') || '').length,
                  skipMfaHandling: retryConfig.__skipMfaHandling,
                })

                // Make NEW request with MFA headers
                const response = await client.request(retryConfig)

                debugLog('New request succeeded', response.status)
                return response
              },
              onResolve: resolve,
              onReject: reject,
            })
          })
        }

        // Handle real 401 MFA errors (fallback for cases we didn't catch)
        if (isMFAError(axiosError)) {
          const originalConfig = (axiosError.config ?? {}) as InternalRequestConfig

          // If this config is marked to skip, don't handle it - let error bubble
          if (originalConfig.__skipMfaHandling === true) {
            debugLog('401 on skip-marked request - rejecting (expected for invalid code)')
            // Suppress toasts since we're handling the error in the form
            if (axiosError.config) {
              ;(axiosError.config as InternalRequestConfig).__suppressGlobalError = true
            }
            axiosError.suppressToast = true
            return Promise.reject(axiosError)
          }

          debugLog('Handling 401 MFA error (fallback)')

          // Suppress toasts for ALL MFA errors since we handle them in the step-up dialog
          if (axiosError.config) {
            ;(axiosError.config as InternalRequestConfig).__suppressGlobalError = true
          }
          axiosError.suppressToast = true

          return new Promise((resolve, reject) => {
            openStepUp({
              action: async () => {
                // Clone headers to avoid mutating original request
                const headers = new AxiosHeaders(originalConfig.headers ?? {})

                const mfaHeaders = getMFAHeaders()
                debugLog('Action getting MFA headers (401 fallback)', mfaHeaders)

                // Add MFA headers
                for (const [key, value] of Object.entries(mfaHeaders)) {
                  if (value) {
                    headers.set(key, value)
                  }
                }

                const retryConfig: InternalRequestConfig = {
                  ...originalConfig,
                  headers,
                  __skipMfaHandling: true,
                }

                debugLog('Retrying request after 401 with MFA headers', {
                  method: retryConfig.method,
                  url: retryConfig.url,
                  hasXMfaTotp: headers.has('X-MFA-TOTP'),
                  xMfaTotpValue: headers.get('X-MFA-TOTP'),
                  skipMfaHandling: retryConfig.__skipMfaHandling,
                })
                const response = await client.request(retryConfig)
                debugLog('Retry succeeded', response.status)
                return response
              },
              onResolve: resolve,
              onReject: reject,
            })
          })
        }

        return Promise.reject(axiosError)
      }
    )

    return () => {
      client.interceptors.request.eject(requestInterceptor)
      client.interceptors.response.eject(responseInterceptor)
    }
  }, [client, openStepUp])

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

  useEffect(() => {
    debugLog('isOpen changed', isOpen)
  }, [isOpen])

  return (
    <StepUpContext.Provider value={contextValue}>
      {children}
      <Dialog
        onOpenChange={(open) => {
          if (open) {
            debugLog('onOpenChange -> open true')
            setIsOpen(true)
          } else {
            debugLog('onOpenChange -> open false')
            closeStepUp()
          }
        }}
        open={isOpen}
      >
        <DialogContent
          className="focus-visible:outline-none"
          onOpenAutoFocus={(e) => {
            e.preventDefault()
            // Blur dropdown to prevent aria-hidden warning
            if (document.activeElement instanceof HTMLElement) {
              document.activeElement.blur()
            }
          }}
        >
          <DialogHeader className="text-center sm:text-center">
            <DialogTitle className="text-center">Multi-Factor Authentication Required</DialogTitle>
          </DialogHeader>

          <DialogDescription className="text-center" />
          <MFAVerificationForm
            mode="step-up"
            onVerify={async (params) => {
              setIsVerifying(true)
              try {
                await handleVerify(params)
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
