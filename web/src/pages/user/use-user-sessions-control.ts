import { useState } from 'react'
import { useMutation } from '@/hooks/use-mutation'
import { api } from '@/lib/api'
import { QUERY_KEYS } from '@/lib/query-keys'
import type { SessionId } from '@/pages/user/session-table'

type ToastApi = {
  error: (message: string) => void
  success: (message: string) => void
}

type UseUserSessionsControlArgs = {
  decodedEmail: string
  queryClient: import('@tanstack/react-query').QueryClient
  toastApi: ToastApi
}

export function useUserSessionsControl({
  decodedEmail,
  queryClient,
  toastApi,
}: UseUserSessionsControlArgs) {
  const [pendingSessionId, setPendingSessionId] = useState<SessionId | null>(null)

  const revokeSessionMutation = useMutation({
    mutationFn: (sessionId: SessionId) => api.revokeUserSession(decodedEmail, sessionId),
    onMutate: (sessionId) => {
      setPendingSessionId(sessionId)
    },
    onSuccess: () => {
      toastApi.success('Session revoked')
      queryClient.invalidateQueries({
        queryKey: [...QUERY_KEYS.USER_SESSIONS, decodedEmail],
      })
    },
    onError: () => {
      toastApi.error('Failed to revoke session')
    },
    onSettled: () => {
      setPendingSessionId(null)
    },
  })

  const revokeAllMutation = useMutation({
    mutationFn: () => api.revokeAllUserSessions(decodedEmail),
    onSuccess: () => {
      toastApi.success('All sessions revoked')
      queryClient.invalidateQueries({
        queryKey: [...QUERY_KEYS.USER_SESSIONS, decodedEmail],
      })
    },
    onError: () => {
      toastApi.error('Failed to revoke sessions')
    },
  })

  return {
    pendingSessionId,
    setPendingSessionId,
    revokeSessionMutation,
    revokeAllMutation,
  }
}
