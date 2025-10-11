import axios, {
  AxiosHeaders,
  type AxiosHeaderValue,
  type AxiosRequestConfig,
  type AxiosResponse,
  type RawAxiosRequestHeaders,
} from 'axios'

import { toast } from '@/components/ui/use-toast'
import { authService, SESSION_EXPIRED_MESSAGE } from '@/lib/auth'

import { getCsrfToken } from '@/lib/csrf'
import { APIRoute } from '@/lib/routes'

const MUTATING_METHODS = new Set(['POST', 'PUT', 'PATCH', 'DELETE'])

const toAxiosHeaders = (value?: AxiosRequestConfig['headers']): AxiosHeaders => {
  if (value instanceof AxiosHeaders) {
    return value
  }
  const headers = new AxiosHeaders()
  if (value) {
    for (const [key, headerValue] of Object.entries(value as RawAxiosRequestHeaders)) {
      if (typeof headerValue !== 'undefined') {
        headers.set(key, headerValue as AxiosHeaderValue)
      }
    }
  }
  return headers
}

export const gatewayAxios = axios.create({
  baseURL: APIRoute(),
  withCredentials: true,
  headers: {
    Accept: 'application/json',
  },
})

gatewayAxios.interceptors.request.use((request) => {
  const method = request.method?.toUpperCase()
  if (method && MUTATING_METHODS.has(method)) {
    const token = getCsrfToken()
    if (token) {
      const headers = toAxiosHeaders(request.headers)
      if (!headers.has('X-CSRF-Token')) {
        headers.set('X-CSRF-Token', token)
      }
      request.headers = headers
    }
  }
  return request
})

gatewayAxios.interceptors.response.use(
  (response) => response,
  (error) => {
    const status = error?.response?.status
    if (status === 401) {
      const dataError = (error?.response?.data as { error?: string } | undefined)?.error
      const mfaRequiredHeader = error?.response?.headers?.['x-mfa-required']
      // Don't treat MFA-required errors as session expiration
      if (
        dataError === 'mfa_step_up_required' ||
        dataError === 'mfa_required' ||
        mfaRequiredHeader === 'step-up' ||
        mfaRequiredHeader === 'always'
      ) {
        return Promise.reject(error)
      }
      const hasWindow = typeof window !== 'undefined'
      if (hasWindow) {
        try {
          window.sessionStorage.setItem('auth_error', SESSION_EXPIRED_MESSAGE)
        } catch (_err) {
          /* ignore */
        }
      }
      try {
        toast.error(SESSION_EXPIRED_MESSAGE)
      } catch (_err) {
        /* ignore */
      }
      authService.logout()
    }
    return Promise.reject(error)
  }
)

export function createGatewayClient<T>(
  config: AxiosRequestConfig,
  options?: AxiosRequestConfig
): Promise<AxiosResponse<T>> {
  const mergedConfig: AxiosRequestConfig = {
    ...config,
    ...options,
  }

  const baseHeaders = toAxiosHeaders(config.headers)
  const overrideHeaders = toAxiosHeaders(options?.headers)
  const overrideEntries = Object.entries(overrideHeaders.toJSON()) as [string, AxiosHeaderValue][]
  for (const [key, value] of overrideEntries) {
    if (typeof value !== 'undefined') {
      baseHeaders.set(key, value)
    }
  }
  mergedConfig.headers = baseHeaders

  return gatewayAxios.request<T>(mergedConfig)
}
