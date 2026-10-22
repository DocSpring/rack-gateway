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
  type StepUpVerification,
  setMFAHeaders,
  verifyStepUp,
} from '@/contexts/step-up-helpers'
import {
  createCancelledError,
  retryQueuedRequests,
  type StepUpAction,
  StepUpQueue,
  type StepUpRequest,
} from '@/contexts/step-up-queue'
import { getMFAStatus } from '@/lib/api'
import type { MFALevel } from '@/lib/generated/mfa-requirements'
import { getMfaRequirementForRequest } from '@/lib/mfa-preflight'

const STEP_UP_BUFFER_MS = 10_000

interface StepUpContextValue {
  openStepUp: (request?: StepUpRequest) => void
  requireStepUp: (action: NonNullable<StepUpAction>) => void
  handleStepUpError: (error: unknown, action: NonNullable<StepUpAction>) => boolean
  runWithMFAGuard: <T>(fn: () => Promise<T>) => Promise<T>
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

  // Every request waiting on the current MFA prompt
  const queueRef = useRef(new StepUpQueue())

  const closeStepUp = useCallback(() => {
    if (isVerifying) {
      return
    }
    debugLog('closeStepUp')
    setIsOpen(false)
    queueRef.current.cancel(createCancelledError())
  }, [isVerifying])

  const openStepUp = useCallback((request?: StepUpRequest) => {
    debugLog('openStepUp', { queued: queueRef.current.size })
    // Blur any active element (like dropdown menus) to prevent focus conflicts
    if (document.activeElement instanceof HTMLElement) {
      document.activeElement.blur()
    }
    queueRef.current.enqueue(request ?? {})
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

  // This is called by the form when the user answers the prompt. A wrong answer throws, so the form shows the error
  // and the prompt stays open for another try.
  const handleVerify = useCallback(
    async (params: StepUpVerification) => {
      const queue = queueRef.current
      const request = queue.primary()
      if (!request?.action) {
        throw new Error('No active MFA request')
      }
      debugLog('handleVerify called', { method: params.method, verify: Boolean(request.verify) })

      if (request.verify) {
        await verifyStepUp(params)
      } else {
        setMFAHeaders(params)
      }

      let settle: () => void
      try {
        // Unless the gateway has already checked the answer, the retried request sends it as its MFA proof
        const result = await runAction(request.action)
        settle = () => request.onResolve?.(result)
      } catch (error) {
        debugLog('handleVerify - action failed', error)
        if (!request.verify && isMFAError(error)) {
          throw error
        }
        // The request failed for another reason: settle it and carry on with the rest of the queue
        settle = () => request.onReject?.(error)
      } finally {
        clearMFAHeaders()
      }

      // Close the dialog, settle this request, then retry everything that was waiting
      const waiting = queue.drain().filter((queued) => queued !== request)
      setIsOpen(false)
      settle()
      const { cancelCount } = queue
      retryQueuedRequests(waiting, {
        run: runAction,
        isMFAError,
        requeue: openStepUp,
        cancelled: () => queue.cancelCount !== cancelCount,
      }).catch((error: unknown) => {
        debugLog('retryQueuedRequests failed', error)
      })
    },
    [openStepUp, runAction]
  )

  const handleDialogVerify = useCallback(
    async (params: StepUpVerification) => {
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
    // step_up_once_enrolled is step_up for a user who has a factor, and nothing for one enrolling their first.
    const shouldPromptForStepUp = async (mfaLevel: MFALevel) => {
      debugLog(`mfaLevel=${mfaLevel} - fetching status to check if prompt needed`)
      const status = await getMFAStatus()
      if (mfaLevel === 'step_up_once_enrolled' && !status.enrolled) {
        return false
      }
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

      if (!isStepUpLevel(requirement.mfaLevel)) {
        return config
      }

      if (await shouldPromptForStepUp(requirement.mfaLevel)) {
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

// isStepUpLevel reports whether a request at this level may need a step-up prompt before it's sent.
function isStepUpLevel(mfaLevel: MFALevel): boolean {
  return mfaLevel === 'step_up' || mfaLevel === 'step_up_once_enrolled'
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
