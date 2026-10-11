import axios, { type AxiosInstance } from 'axios'
import { createContext, type PropsWithChildren, useContext, useEffect, useMemo } from 'react'

import { getCsrfToken } from '@/lib/csrf'
import { APIRoute } from '@/lib/routes'

type HttpClientContextValue = {
  client: AxiosInstance
}

const HttpClientContext = createContext<HttpClientContextValue | null>(null)

const MUTATING_METHODS = new Set(['POST', 'PUT', 'PATCH', 'DELETE'])

let sharedClient: AxiosInstance | null = null

const ensureClient = (): AxiosInstance => {
  if (!sharedClient) {
    sharedClient = axios.create({
      baseURL: APIRoute(),
      withCredentials: true,
      headers: {
        Accept: 'application/json',
      },
    })
  }
  return sharedClient
}

export function getHttpClientInstance(): AxiosInstance {
  if (!sharedClient) {
    throw new Error('HTTP client has not been initialised. Wrap your app with HttpClientProvider.')
  }
  return sharedClient
}

export function HttpClientProvider({ children }: PropsWithChildren): React.ReactElement {
  const client = useMemo(ensureClient, [])

  useEffect(() => {
    const requestInterceptor = client.interceptors.request.use((request) => {
      const method = request.method?.toUpperCase()
      if (method && MUTATING_METHODS.has(method)) {
        const token = getCsrfToken()
        if (token && !request.headers?.['X-CSRF-Token']) {
          const headers = axios.AxiosHeaders.from(request.headers)
          headers.set('X-CSRF-Token', token)
          request.headers = headers
        }
      }
      return request
    })

    return () => {
      client.interceptors.request.eject(requestInterceptor)
    }
  }, [client])

  const value = useMemo<HttpClientContextValue>(() => ({ client }), [client])

  return <HttpClientContext.Provider value={value}>{children}</HttpClientContext.Provider>
}

export function useHttpClient(): HttpClientContextValue {
  const ctx = useContext(HttpClientContext)
  if (!ctx) {
    throw new Error('useHttpClient must be used within an HttpClientProvider')
  }
  return ctx
}
