import type { AxiosError } from 'axios'
import type React from 'react'
import { useEffect } from 'react'

import { toast } from '@/components/ui/use-toast'
import { authService, SESSION_EXPIRED_MESSAGE } from '@/lib/auth'
import { toastAPIError } from '@/lib/error-utils'

import { useHttpClient } from './http-client-context'

type InterceptorError = AxiosError & { suppressToast?: boolean }

const shouldSuppressError = (error: InterceptorError): boolean => {
  const suppressGlobal = (error?.config as { __suppressGlobalError?: boolean } | undefined)
    ?.__suppressGlobalError
  return Boolean(suppressGlobal || error.suppressToast)
}

const handleUnauthorized = (error: InterceptorError): boolean => {
  const status = error.response?.status
  if (status !== 401) {
    return false
  }
  const errorCode = (error.response?.data as { error?: string } | undefined)?.error
  if (errorCode === 'mfa_verification_failed') {
    return false
  }
  if (typeof window !== 'undefined') {
    try {
      window.sessionStorage.setItem('auth_error', SESSION_EXPIRED_MESSAGE)
    } catch {
      /* ignore storage failures */
    }
  }
  toast.error(SESSION_EXPIRED_MESSAGE)
  authService.logout()
  return true
}

const handleApiError = (rawError: unknown): Promise<never> => {
  const error = rawError as InterceptorError
  if (shouldSuppressError(error)) {
    return Promise.reject(error)
  }
  if (handleUnauthorized(error)) {
    return Promise.reject(error)
  }
  toastAPIError(error)
  return Promise.reject(error)
}

export function ApiErrorProvider({ children }: { children: React.ReactNode }): React.ReactElement {
  const { client } = useHttpClient()

  useEffect(() => {
    const interceptorId = client.interceptors.response.use((response) => response, handleApiError)

    return () => {
      client.interceptors.response.eject(interceptorId)
    }
  }, [client])

  return <>{children}</>
}
