import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useParams } from '@tanstack/react-router'
import { RefreshCw } from 'lucide-react'
import { useMemo, useState } from 'react'
import { toast } from '@/components/ui/use-toast'
import { TimeAgo } from '../components/time-ago'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '../components/ui/table'
import type { AuditLogEntry, GatewayUser, UserSessionSummary } from '../lib/api'
import { AVAILABLE_ROLES, api } from '../lib/api'

const STATUS_COLORS: Record<string, string> = {
  success: 'bg-green-600 text-white',
  error: 'bg-destructive text-destructive-foreground',
  failed: 'bg-destructive text-destructive-foreground',
  denied: 'bg-yellow-600 text-white',
  blocked: 'bg-zinc-700 text-white',
}

function roleBadges(roles: string[]) {
  return roles.map((role) => {
    const cfg = AVAILABLE_ROLES[role as keyof typeof AVAILABLE_ROLES]
    return (
      <Badge key={role} variant="outline">
        {cfg?.name ?? role}
      </Badge>
    )
  })
}

function SessionTable({
  sessions,
  onRevoke,
  pendingSessionId,
  loading,
  error,
  disableActions,
}: {
  sessions: UserSessionSummary[]
  onRevoke: (sessionId: number) => void
  pendingSessionId: number | null
  loading: boolean
  error?: string | null
  disableActions: boolean
}) {
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
            <TableHead>IP</TableHead>
            <TableHead>User Agent</TableHead>
            <TableHead className="text-right">Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {sessions.map((session) => (
            <TableRow key={session.id}>
              <TableCell className="text-sm">
                <TimeAgo date={session.created_at} />
              </TableCell>
              <TableCell className="text-sm">
                <TimeAgo date={session.last_seen_at} />
              </TableCell>
              <TableCell className="text-sm">
                <TimeAgo date={session.expires_at} />
              </TableCell>
              <TableCell className="font-mono text-sm">{session.ip_address || '—'}</TableCell>
              <TableCell className="max-w-[220px] truncate text-sm" title={session.user_agent}>
                {session.user_agent || '—'}
              </TableCell>
              <TableCell className="text-right">
                <Button
                  disabled={disableActions || pendingSessionId === session.id}
                  onClick={() => onRevoke(session.id)}
                  size="sm"
                  variant="outline"
                >
                  {pendingSessionId === session.id ? (
                    <RefreshCw className="h-4 w-4 animate-spin" />
                  ) : (
                    'Sign Out'
                  )}
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function RecentActivity({
  logs,
  loading,
  error,
  email,
}: {
  logs: AuditLogEntry[]
  loading: boolean
  error?: string | null
  email: string
}) {
  if (loading) {
    return <p className="text-muted-foreground text-sm">Loading activity…</p>
  }
  if (error) {
    return <p className="text-destructive text-sm">{error}</p>
  }
  if (logs.length === 0) {
    return <p className="text-muted-foreground text-sm">No recent activity.</p>
  }
  return (
    <div className="space-y-3">
      <ul className="space-y-3">
        {logs.map((log) => {
          const statusKey = (log.status || '').toLowerCase()
          return (
            <li className="rounded border border-border p-3" key={log.id}>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2 text-sm">
                  <TimeAgo date={log.timestamp} />
                  <Badge
                    className={STATUS_COLORS[statusKey] ?? 'bg-muted text-muted-foreground'}
                    variant="outline"
                  >
                    {log.status}
                  </Badge>
                </div>
                <span className="font-mono text-muted-foreground text-xs">{log.action_type}</span>
              </div>
              <div className="mt-2 font-medium text-sm">{log.action}</div>
              {log.resource && (
                <div className="text-muted-foreground text-xs">Resource: {log.resource}</div>
              )}
            </li>
          )
        })}
      </ul>
      <Button asChild size="sm" variant="outline">
        <Link params={{ email }} to="/users/$email/audit_logs">
          View Full Audit Log
        </Link>
      </Button>
    </div>
  )
}

export function UserPage() {
  const { email } = useParams({ from: '/users/$email' }) as { email: string }
  const decodedEmail = useMemo(() => decodeURIComponent(email), [email])
  const queryClient = useQueryClient()
  const [pendingSessionId, setPendingSessionId] = useState<number | null>(null)

  const {
    data: user,
    isLoading: userLoading,
    error: userError,
  } = useQuery<GatewayUser, Error>({
    queryKey: ['user', decodedEmail],
    queryFn: () => api.getUser(decodedEmail),
    retry: 1,
  })

  const {
    data: sessions = [],
    isLoading: sessionsLoading,
    error: sessionsError,
  } = useQuery<UserSessionSummary[], Error>({
    queryKey: ['userSessions', decodedEmail],
    queryFn: () => api.getUserSessions(decodedEmail),
    enabled: !!user,
    refetchOnWindowFocus: true,
  })

  const {
    data: auditLogs = [],
    isLoading: auditLoading,
    error: auditError,
  } = useQuery<AuditLogEntry[], Error>({
    queryKey: ['userAuditPreview', decodedEmail],
    queryFn: () => api.getUserAuditLogs(decodedEmail, 10),
    enabled: !!user,
    staleTime: 30_000,
  })

  const revokeSessionMutation = useMutation({
    mutationFn: (sessionId: number) => api.revokeUserSession(decodedEmail, sessionId),
    onMutate: (sessionId) => {
      setPendingSessionId(sessionId)
    },
    onSuccess: () => {
      toast.success('Session revoked')
      queryClient.invalidateQueries({ queryKey: ['userSessions', decodedEmail] })
    },
    onError: () => {
      toast.error('Failed to revoke session')
    },
    onSettled: () => {
      setPendingSessionId(null)
    },
  })

  const revokeAllMutation = useMutation({
    mutationFn: () => api.revokeAllUserSessions(decodedEmail),
    onSuccess: () => {
      toast.success('All sessions revoked')
      queryClient.invalidateQueries({ queryKey: ['userSessions', decodedEmail] })
    },
    onError: () => {
      toast.error('Failed to revoke sessions')
    },
  })

  if (userError) {
    return (
      <div className="space-y-4">
        <h1 className="font-semibold text-2xl">User</h1>
        <p className="text-destructive text-sm">Unable to load user: {userError.message}</p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h1 className="font-semibold text-3xl">
            {userLoading ? 'Loading…' : user?.name || decodedEmail}
          </h1>
          <p className="text-muted-foreground">{decodedEmail}</p>
          {user && user.roles.length > 0 && (
            <div className="mt-2 flex flex-wrap gap-2">{roleBadges(user.roles)}</div>
          )}
        </div>
        <div className="flex gap-2">
          <Button asChild variant="outline">
            <Link params={{ email }} to="/users/$email/audit_logs">
              View Audit Logs
            </Link>
          </Button>
          <Button
            disabled={revokeAllMutation.isPending || userLoading || sessions.length === 0}
            onClick={() => revokeAllMutation.mutate()}
            variant="destructive"
          >
            {revokeAllMutation.isPending && <RefreshCw className="mr-2 h-4 w-4 animate-spin" />}
            Sign Out Everywhere
          </Button>
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Active Sessions</CardTitle>
          </CardHeader>
          <CardContent>
            <SessionTable
              disableActions={revokeAllMutation.isPending}
              error={sessionsError ? 'Failed to load sessions' : null}
              loading={sessionsLoading || userLoading}
              onRevoke={(id) => revokeSessionMutation.mutate(id)}
              pendingSessionId={pendingSessionId}
              sessions={sessions}
            />
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Recent Activity</CardTitle>
          </CardHeader>
          <CardContent>
            <RecentActivity
              email={email}
              error={auditError ? 'Failed to load activity' : null}
              loading={auditLoading || userLoading}
              logs={auditLogs}
            />
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
