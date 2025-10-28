import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import type { UserSessionSummary } from '@/lib/api'
import type { SessionId } from '@/pages/user/session-table'
import { SessionTable } from '@/pages/user/session-table'

type UserSessionsCardProps = {
  sessions: UserSessionSummary[]
  sessionsError: Error | null
  sessionsLoading: boolean
  userLoading: boolean
  pendingSessionId: SessionId | null
  onRevoke: (id: SessionId) => void
  revokeAllPending: boolean
}

export function UserSessionsCard({
  sessions,
  sessionsError,
  sessionsLoading,
  userLoading,
  pendingSessionId,
  onRevoke,
  revokeAllPending,
}: UserSessionsCardProps) {
  return (
    <Card data-testid="user-sessions-card">
      <CardHeader>
        <CardTitle>Active Sessions</CardTitle>
      </CardHeader>
      <CardContent>
        <SessionTable
          disableActions={revokeAllPending}
          error={sessionsError ? 'Failed to load sessions' : null}
          loading={sessionsLoading || userLoading}
          onRevoke={onRevoke}
          pendingSessionId={pendingSessionId}
          sessions={sessions}
        />
      </CardContent>
    </Card>
  )
}
