import axios, {
  AxiosHeaders,
  type AxiosHeaderValue,
  type AxiosRequestConfig,
  type AxiosResponse,
  type RawAxiosRequestHeaders,
} from 'axios'

import { toast } from '@/components/ui/use-toast'
import { authService } from '@/lib/auth'

import { getCsrfToken } from '@/lib/csrf'
import { APIRoute } from '@/lib/routes'

const MUTATING_METHODS = new Set(['POST', 'PUT', 'PATCH', 'DELETE'])

const rawApiBaseUrl =
  typeof import.meta.env.VITE_API_BASE_URL === 'string'
    ? import.meta.env.VITE_API_BASE_URL.trim()
    : ''

const normalizedOrigin = rawApiBaseUrl.replace(/\/+$/, '')

let apiBaseUrl = APIRoute()
if (normalizedOrigin) {
  apiBaseUrl = normalizedOrigin.endsWith(APIRoute())
    ? normalizedOrigin
    : `${normalizedOrigin}${APIRoute()}`
}

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
  baseURL: apiBaseUrl,
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
      const hasWindow = typeof window !== 'undefined'
      if (hasWindow) {
        try {
          window.sessionStorage.setItem('auth_error', 'Unauthorized. Please sign in to continue.')
        } catch (_err) {
          /* ignore */
        }
      }
      try {
        toast.error('Unauthorized. Please sign in to continue.')
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

  const baseHeaders = AxiosHeaders.from(toAxiosHeaders(config.headers).toJSON())
  toAxiosHeaders(options?.headers).forEach((value: AxiosHeaderValue, key: string) => {
    baseHeaders.set(key, value)
  })
  mergedConfig.headers = baseHeaders

  return gatewayAxios.request<T>(mergedConfig)
}
