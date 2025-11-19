import type { AxiosError } from 'axios'
import type React from 'react'
import { useEffect } from 'react'

import { toast } from '@/components/ui/use-toast'
import { authService, SESSION_EXPIRED_MESSAGE } from '@/lib/auth'
import { toastAPIError } from '@/lib/error-utils'

import { useHttpClient } from './http-client-context'

type InterceptorError = AxiosError & { suppressToast?: boolean }

// Pre-compiled regex for performance (used in handleUnauthorized)
const TRAILING_SLASH_REGEX = /\/$/
const MULTIPLE_SLASHES_REGEX = /\/+/g

const shouldSuppressError = (error: InterceptorError): boolean => {
  const suppressGlobal = (error?.config as { __suppressGlobalError?: boolean } | undefined)
    ?.__suppressGlobalError
  if (suppressGlobal || error.suppressToast) {
    return true
  }

  // Suppress errors from MFA verification endpoints - they're handled locally with inline errors
  // But DON'T suppress /auth/mfa/status - we want 401s from that to trigger logout
  const url = error.config?.url ?? ''
  if (
    url.includes('/auth/mfa/webauthn/assertion/verify') ||
    url.includes('/auth/mfa/verify') ||
    (url.includes('/auth/mfa/') && url.includes('/start'))
  ) {
    return true
  }

  return false
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

  // Redirect to login with returnTo parameter containing current path
  // This allows redirecting back after successful authentication
  if (typeof window !== 'undefined') {
    const currentPath = window.location.pathname
    const base = import.meta.env.BASE_URL || '/'
    const loginPath = `${base}login`.replace(MULTIPLE_SLASHES_REGEX, '/')
    const normalizedBase = base === '/' ? '' : base.replace(TRAILING_SLASH_REGEX, '')

    // Only include returnTo if we're on an actual app page (not already on login/auth pages)
    const isAuthPage = currentPath === loginPath || currentPath.includes('/auth/')
    if (!isAuthPage && currentPath !== normalizedBase && currentPath !== `${normalizedBase}/`) {
      const returnTo = currentPath
      const loginURL = `${loginPath}?returnTo=${encodeURIComponent(returnTo)}`
      window.location.href = loginURL
      return true
    }
  }

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
  // Don't show toasts for 404 errors - they're often expected (e.g., checking if resource exists)
  const status = error.response?.status
  if (status === 404) {
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
