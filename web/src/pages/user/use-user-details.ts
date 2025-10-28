import { useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'

import type { GatewayUser } from '@/lib/api'
import { api } from '@/lib/api'
import { QUERY_KEYS } from '@/lib/query-keys'
import { pickPrimaryRole } from '@/lib/user-roles'

export function useUserDetails(email: string) {
  const {
    data: user,
    isLoading: userLoading,
    error: userError,
  } = useQuery<GatewayUser, Error>({
    queryKey: [...QUERY_KEYS.USER, email],
    queryFn: () => api.getUser(email),
    retry: 1,
  })

  const currentPrimaryRole = useMemo(() => pickPrimaryRole(user?.roles ?? []), [user?.roles])

  return {
    user,
    userLoading,
    userError,
    currentPrimaryRole,
  }
}
