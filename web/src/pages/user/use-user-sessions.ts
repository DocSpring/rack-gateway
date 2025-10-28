import { useQuery } from '@tanstack/react-query'

import type { UserSessionSummary } from '@/lib/api'
import { api } from '@/lib/api'
import { QUERY_KEYS } from '@/lib/query-keys'

export function useUserSessions(email: string, enabled: boolean) {
  const {
    data: sessions = [],
    isLoading: sessionsLoading,
    error: sessionsError,
  } = useQuery<UserSessionSummary[], Error>({
    queryKey: [...QUERY_KEYS.USER_SESSIONS, email],
    queryFn: () => api.listUserSessions(email),
    enabled,
    refetchOnWindowFocus: true,
  })

  return {
    sessions,
    sessionsLoading,
    sessionsError,
  }
}
