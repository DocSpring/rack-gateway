import {
  type AxiosError,
  AxiosHeaders,
  type AxiosInstance,
  type AxiosResponse,
  type InternalAxiosRequestConfig,
} from 'axios'
import type { ReactNode } from 'react'
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'

import { useAuth } from '@/contexts/auth-context'
import { useHttpClient } from '@/contexts/http-client-context'
import { debugLog } from '@/contexts/step-up-debug'
import { StepUpDialog } from '@/contexts/step-up-dialog'
import {
  clearMFAHeaders,
  getMFAHeaders,
  isMFAError,
  setTotpHeader,
  setWebAuthnHeader,
} from '@/contexts/step-up-helpers'
import { getMFAStatus } from '@/lib/api'
import { getMfaRequirementForRequest } from '@/lib/mfa-preflight'

const STEP_UP_BUFFER_MS = 10_000

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
    debugLog('runAction invoked', Boolean(action))
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
        setTotpHeader(params.code)
        debugLog('Set TOTP header', { code: params.code })
      } else {
        setWebAuthnHeader({
          session_data: params.session_data,
          assertion_response: params.assertion_response,
        })
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

  const handleDialogVerify = useCallback(
    async (params: {
      method: 'totp' | 'webauthn'
      code?: string
      trust_device: boolean
      session_data?: string
      assertion_response?: string
    }) => {
      setIsVerifying(true)
      try {
        await handleVerify(params)
      } finally {
        setIsVerifying(false)
      }
    },
    [handleVerify]
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

  const handleDialogOpenChange = useCallback(
    (open: boolean) => {
      if (open) {
        debugLog('onOpenChange -> open true')
        setIsOpen(true)
      } else {
        debugLog('onOpenChange -> open false')
        closeStepUp()
      }
    },
    [closeStepUp]
  )

  useEffect(() => {
    const shouldPromptForStepUp = async () => {
      debugLog('mfaLevel=step_up - fetching status to check if prompt needed')
      const status = await getMFAStatus()
      const expiresAt = status.recent_step_up_expires_at
        ? new Date(status.recent_step_up_expires_at)
        : null

      const needsPrompt = !expiresAt || expiresAt.getTime() - Date.now() <= STEP_UP_BUFFER_MS

      debugLog('step_up check', {
        expiresAt: expiresAt?.toISOString(),
        needsPrompt,
      })

      return needsPrompt
    }

    const handleRequest = async (config: InternalRequestConfig) => {
      debugLog('Request interceptor checking config', {
        method: config.method,
        url: config.url,
        hasSkipFlag: config.__skipMfaHandling === true,
      })

      if (config.__skipMfaHandling === true) {
        debugLog('Request marked as skip - allowing through')
        return config
      }

      const requirement = resolveRequirement(client, config)
      if (!requirement) {
        return config
      }

      debugLog('applyMfaRequirement', {
        method: config.method,
        url: config.url,
        mfaLevel: requirement.mfaLevel,
      })

      if (requirement.mfaLevel === 'always') {
        debugLog('mfaLevel=always - aborting request to prompt for MFA')
        return abortForMfa(config)
      }

      if (requirement.mfaLevel !== 'step_up') {
        return config
      }

      if (await shouldPromptForStepUp()) {
        debugLog('step_up expired - aborting request to prompt for MFA')
        return abortForMfa(config)
      }

      debugLog('step_up valid - proceeding with request')
      return config
    }

    const handleResponseError = async (error: unknown) => {
      const axiosError = error as AxiosStepUpError

      if (isAbortForMfaError(axiosError)) {
        debugLog('Handling aborted request - opening step-up dialog')
        return await createStepUpPromise(client, axiosError, openStepUp)
      }

      if (!isMFAError(axiosError)) {
        throw axiosError
      }

      suppressMfaError(axiosError)

      const originalConfig = (axiosError.config ?? {}) as InternalRequestConfig
      if (originalConfig.__skipMfaHandling === true) {
        debugLog('401 on skip-marked request - rejecting (expected for invalid code)')
        throw axiosError
      }

      debugLog('Handling 401 MFA error (fallback)')
      return await createStepUpPromise(client, axiosError, openStepUp)
    }

    const requestInterceptor = client.interceptors.request.use(handleRequest)
    const responseInterceptor = client.interceptors.response.use(
      (response) => response,
      (error) => handleResponseError(error)
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
      <StepUpDialog
        isOpen={isOpen}
        onCancel={closeStepUp}
        onOpenChange={handleDialogOpenChange}
        onVerify={handleDialogVerify}
        showTrustDevice={!user?.has_trusted_device}
      />
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

type AxiosStepUpError = AxiosError<unknown, InternalRequestConfig> & {
  suppressToast?: boolean
}

function resolveRequirement(client: AxiosInstance, config: InternalRequestConfig) {
  const baseUrl =
    typeof config.baseURL === 'string'
      ? config.baseURL
      : (client.defaults.baseURL as string | undefined)
  return getMfaRequirementForRequest(config.method?.toUpperCase(), config.url, baseUrl)
}

function abortForMfa(config: InternalRequestConfig): InternalRequestConfig {
  const controller = new AbortController()
  controller.abort()
  return {
    ...config,
    signal: controller.signal,
    __abortedForMfa: true,
  }
}

function isAbortForMfaError(error: AxiosStepUpError): boolean {
  const config = error.config as InternalRequestConfig | undefined
  return error.code === 'ERR_CANCELED' && Boolean(config?.__abortedForMfa)
}

function suppressMfaError(error: AxiosStepUpError): void {
  const config = error.config as InternalRequestConfig | undefined
  if (config) {
    config.__suppressGlobalError = true
  }
  error.suppressToast = true
}

async function retryRequestWithMfa(
  client: AxiosInstance,
  originalConfig: InternalRequestConfig
): Promise<AxiosResponse> {
  const headers = new AxiosHeaders(originalConfig.headers ?? {})
  const mfaHeaders = getMFAHeaders()

  debugLog('Retrying request with MFA headers', {
    method: originalConfig.method,
    url: originalConfig.url,
    hasXMfaTotp: Boolean(mfaHeaders['X-MFA-TOTP']),
    hasXMfaWebAuthn: Boolean(mfaHeaders['X-MFA-WebAuthn']),
  })

  for (const [key, value] of Object.entries(mfaHeaders)) {
    if (value) {
      headers.set(key, value)
    }
  }

  const retryConfig: InternalRequestConfig = {
    ...originalConfig,
    headers,
    signal: undefined,
    __abortedForMfa: undefined,
    __skipMfaHandling: true,
  }

  const response = await client.request(retryConfig)
  debugLog('Retry succeeded', response.status)
  return response
}

function createStepUpPromise(
  client: AxiosInstance,
  error: AxiosStepUpError,
  openStepUp: StepUpContextValue['openStepUp']
): Promise<AxiosResponse> {
  const originalConfig = (error.config ?? {}) as InternalRequestConfig | undefined
  if (!originalConfig) {
    return Promise.reject(error)
  }

  return new Promise((resolve, reject) => {
    openStepUp({
      action: async () => retryRequestWithMfa(client, originalConfig),
      onResolve: (value) => resolve(value as AxiosResponse),
      onReject: reject,
    })
  })
}
