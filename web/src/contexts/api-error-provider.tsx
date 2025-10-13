import React, { useEffect } from 'react'

import { toast } from '@/components/ui/use-toast'
import { toastAPIError } from '@/lib/error-utils'
import { authService, SESSION_EXPIRED_MESSAGE } from '@/lib/auth'

import { useHttpClient } from './http-client-context'

export function ApiErrorProvider({ children }: { children: React.ReactNode }): React.ReactElement {
  const { client } = useHttpClient()

  useEffect(() => {
    const interceptorId = client.interceptors.response.use(
      (response) => response,
      (error) => {
        const suppressGlobal = (error?.config as { __suppressGlobalError?: boolean } | undefined)
          ?.__suppressGlobalError
        const suppressToast = (error as { suppressToast?: boolean } | undefined)?.suppressToast

        if (suppressGlobal || suppressToast) {
          return Promise.reject(error)
        }

        const status = error?.response?.status
        if (status === 401) {
          if (typeof window !== 'undefined') {
            try {
              window.sessionStorage.setItem('auth_error', SESSION_EXPIRED_MESSAGE)
            } catch (_err) {
              /* ignore */
            }
          }
          toast.error(SESSION_EXPIRED_MESSAGE)
          authService.logout()
          return Promise.reject(error)
        }

        toastAPIError(error)
        return Promise.reject(error)
      }
    )

    return () => {
      client.interceptors.response.eject(interceptorId)
    }
  }, [client])

  return <>{children}</>
}
