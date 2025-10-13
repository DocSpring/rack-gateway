import { useParams } from '@tanstack/react-router'
import { AuditPage } from './audit-page'

// Thin wrapper around AuditPage that filters by user email derived from the path.
export function UserAuditPage() {
  const { email } = useParams({ from: '/users/$email/audit-logs' }) as {
    email: string
  }
  const decodedEmail = decodeURIComponent(email)
  return <AuditPage userEmail={decodedEmail} />
}
