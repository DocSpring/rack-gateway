import { RefreshCw } from 'lucide-react'
import { TimeAgo } from '@/components/time-ago'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import type { UserSessionSummary } from '@/lib/api'

export type SessionId = NonNullable<UserSessionSummary['id']>

type SessionTableProps = {
  sessions: UserSessionSummary[]
  onRevoke: (sessionId: SessionId) => void
  pendingSessionId: SessionId | null
  loading: boolean
  error?: string | null
  disableActions: boolean
}

export function SessionTable({
  sessions,
  onRevoke,
  pendingSessionId,
  loading,
  error,
  disableActions,
}: SessionTableProps) {
  if (loading) {
    return <p className="text-muted-foreground text-sm">Loading sessions…</p>
  }

  if (error) {
    return <p className="text-destructive text-sm">{error}</p>
  }

  if (sessions.length === 0) {
    return <p className="text-muted-foreground text-sm">No active sessions.</p>
  }

  return (
    <div className="space-y-3">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Created</TableHead>
            <TableHead>Last Seen</TableHead>
            <TableHead>Expires</TableHead>
            <TableHead>Client</TableHead>
            <TableHead>IP</TableHead>
            <TableHead>User Agent</TableHead>
            <TableHead className="text-right">Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {sessions.map((session, index) => {
            const sessionId = session.id
            const rowKey = sessionId ?? `session-${index}`
            return (
              <TableRow key={rowKey}>
                <TableCell className="text-sm">
                  <TimeAgo date={session.created_at ?? null} />
                </TableCell>
                <TableCell className="text-sm">
                  <TimeAgo date={session.last_seen_at ?? null} />
                </TableCell>
                <TableCell className="text-sm">
                  <TimeAgo date={session.expires_at ?? null} />
                </TableCell>
                <TableCell className="text-sm">
                  {session.channel === 'cli' ? 'CLI' : 'Browser'}
                </TableCell>
                <TableCell className="font-mono text-sm">{session.ip_address || '—'}</TableCell>
                <TableCell className="max-w-[220px] truncate text-sm" title={session.user_agent}>
                  {session.user_agent || '—'}
                </TableCell>
                <TableCell className="text-right">
                  <Button
                    disabled={
                      disableActions || (sessionId !== undefined && pendingSessionId === sessionId)
                    }
                    onClick={() => {
                      if (sessionId !== undefined) {
                        onRevoke(sessionId)
                      }
                    }}
                    size="sm"
                    variant="destructive"
                  >
                    {sessionId !== undefined && pendingSessionId === sessionId ? (
                      <RefreshCw className="h-4 w-4 animate-spin" />
                    ) : (
                      'Sign Out'
                    )}
                  </Button>
                </TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    </div>
  )
}
