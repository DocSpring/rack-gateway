import { useCallback } from 'react'
import { useAuth } from '@/contexts/auth-context'
import { hasPermission } from '@/lib/permissions'

// useCan returns a check for the signed-in user's permissions (see lib/permissions).
export function useCan(): (permission: string) => boolean {
  const { user } = useAuth()
  const permissions = user?.permissions
  return useCallback((permission: string) => hasPermission(permissions, permission), [permissions])
}
