import { useQuery } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { api } from '../lib/api'
import { AuditPage } from './audit-page'

// Thin wrapper around AuditPage that passes a userId prop derived from the path.
export function UserAuditPage() {
  const { id } = useParams({ from: '/users/$id/audit_logs' }) as { id: string }
  const { data: users = [] } = useQuery({
    queryKey: ['users-for-audit'],
    queryFn: async () =>
      api.get<Array<{ id: number; email: string; name: string }>>('/.gateway/api/admin/users'),
    staleTime: 30_000,
  })
  const uid = Number.parseInt(id, 10)
  const u = users.find((x) => x.id === uid)
  const userEmail = u?.email
  return <AuditPage userEmail={userEmail} userId={id} />
}
