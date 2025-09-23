import { Eye } from 'lucide-react'
import { useMemo, useState } from 'react'
import { TablePane } from './table-pane'
import { TimeAgo } from './time-ago'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from './ui/dialog'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from './ui/table'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip'

export type AuditLogRecord = {
  id: number
  timestamp: string
  user_email: string
  user_name?: string
  action_type: string
  action: string
  command?: string
  resource?: string
  resource_type?: string
  status: string
  details?: string
  ip_address?: string
  user_agent?: string
  http_status?: number
  response_time_ms: number
  rbac_decision?: string
}

export type AuditLogsPaneProps = {
  title: string
  logs: AuditLogRecord[]
  totalCount: number
  currentPage: number
  totalPages: number
  firstRowIndex: number
  lastRowIndex: number
  loading: boolean
  error?: string | null
  emptyMessage?: string
  onPreviousPage: () => void
  onNextPage: () => void
  disablePrevious?: boolean
  disableNext?: boolean
}

const MAX_LABEL_LEN = 23

function safeParseDetails(details: string | undefined): Record<string, unknown> {
  if (!details) {
    return {}
  }
  try {
    return JSON.parse(details) as Record<string, unknown>
  } catch {
    return {}
  }
}

function resourceLabelForLog(log: AuditLogRecord): string {
  const details = safeParseDetails(log.details)
  let label = ''
  if (log.action_type === 'users' || log.action.startsWith('user.')) {
    label = (details.email as string) || ''
  } else if (log.action_type === 'tokens' || log.action.startsWith('api_token.')) {
    label = (details.name as string) || ''
  }
  if (!label) {
    label = (log.resource || '').trim() || '-'
  }
  return label
}

function LabelBadge({ label }: { label: string }) {
  const needsTruncate = label.length > MAX_LABEL_LEN
  const shortText = needsTruncate ? `${label.slice(0, MAX_LABEL_LEN - 3)}...` : label
  const content = (
    <Badge
      className="border border-border bg-muted font-mono text-muted-foreground"
      variant="outline"
    >
      {shortText || '-'}
    </Badge>
  )
  if (!needsTruncate) {
    return content
  }
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>{content}</TooltipTrigger>
        <TooltipContent>
          <span className="font-mono">{label}</span>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}

function getStatusBadgeAppearance(status: string): {
  variant: 'default' | 'secondary' | 'destructive' | 'outline'
  className?: string
} {
  switch (status) {
    case 'success':
      return { variant: 'default', className: 'bg-green-600 text-white hover:bg-green-700' }
    case 'failed':
    case 'error':
    case 'blocked':
    case 'denied':
      return { variant: 'destructive' }
    default:
      return { variant: 'outline' }
  }
}

function getActionTypeBadgeAppearance(type: string): {
  variant: 'default' | 'secondary' | 'destructive' | 'outline'
  className?: string
} {
  switch (type) {
    case 'auth':
      return { variant: 'outline', className: 'bg-blue-600 text-white border border-border' }
    case 'users':
      return { variant: 'outline', className: 'bg-purple-600 text-white border border-border' }
    case 'tokens':
      return { variant: 'outline', className: 'bg-amber-600 text-white border border-border' }
    case 'convox':
      return { variant: 'outline', className: 'bg-slate-700 text-white border border-border' }
    default:
      return {
        variant: 'outline',
        className: 'bg-muted text-muted-foreground border border-border',
      }
  }
}

function getResourceTypeBadgeAppearance(type: string): {
  variant: 'default' | 'secondary' | 'destructive' | 'outline'
  className?: string
} {
  switch (type) {
    case 'app':
      return { variant: 'outline', className: 'bg-indigo-600 text-white border border-border' }
    case 'rack':
      return { variant: 'outline', className: 'bg-emerald-600 text-white border border-border' }
    case 'env':
      return { variant: 'outline', className: 'bg-orange-500 text-white border border-border' }
    case 'api_token':
      return { variant: 'outline', className: 'bg-rose-600 text-white border border-border' }
    case 'user':
    case 'auth':
      return { variant: 'default', className: 'bg-blue-600 text-white' }
    default:
      return {
        variant: 'outline',
        className: 'bg-muted text-muted-foreground border border-border',
      }
  }
}

function extractExecCommand(log: AuditLogRecord): string {
  const raw = (() => {
    try {
      const parsed = JSON.parse(log.details || '{}') as { command?: string }
      return (log.command || parsed.command || '').trim()
    } catch {
      return (log.command || '').trim()
    }
  })()
  if ((raw.startsWith("'") && raw.endsWith("'")) || (raw.startsWith('"') && raw.endsWith('"'))) {
    return raw.slice(1, -1)
  }
  return raw
}

function renderActionCell(log: AuditLogRecord) {
  if (log.action_type === 'convox' && log.action === 'process.exec') {
    const command = extractExecCommand(log)
    const truncated = command.length > 64 ? `${command.slice(0, 64)}…` : command
    return (
      <div className="flex flex-col">
        <Badge
          className="w-fit border border-border bg-muted font-mono text-muted-foreground"
          variant="outline"
        >
          {log.action}
        </Badge>
        {command && (
          <code
            className="mt-1 w-fit whitespace-nowrap rounded border border-border bg-secondary px-1 py-0.5 font-mono text-blue-600 shadow-sm dark:text-blue-300"
            title={command}
          >
            {truncated}
          </code>
        )}
      </div>
    )
  }

  return (
    <Badge
      className="border border-border bg-muted font-mono text-muted-foreground"
      variant="outline"
    >
      {log.action}
    </Badge>
  )
}

export function AuditLogsPane({
  title,
  logs,
  totalCount,
  currentPage,
  totalPages,
  firstRowIndex,
  lastRowIndex,
  loading,
  error,
  emptyMessage = 'No audit logs found',
  onPreviousPage,
  onNextPage,
  disablePrevious = false,
  disableNext = false,
}: AuditLogsPaneProps) {
  const [selected, setSelected] = useState<AuditLogRecord | null>(null)

  const description = useMemo(() => {
    if (logs.length === 0) {
      return 'No audit logs'
    }
    return `Showing ${firstRowIndex === 0 ? 0 : firstRowIndex}–${lastRowIndex} of ${totalCount} logs`
  }, [firstRowIndex, lastRowIndex, logs.length, totalCount])

  const handleRowClick = (log: AuditLogRecord) => {
    setSelected(log)
  }

  return (
    <>
      <TablePane
        description={description}
        empty={logs.length === 0}
        emptyMessage={emptyMessage}
        error={error ?? null}
        loading={loading}
        title={title}
      >
        <Table className="text-sm">
          <TableHeader>
            <TableRow>
              <TableHead>User</TableHead>
              <TableHead>Type</TableHead>
              <TableHead>Action</TableHead>
              <TableHead>Resource Type</TableHead>
              <TableHead>Resource</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>IP Address</TableHead>
              <TableHead>Timestamp</TableHead>
              <TableHead className="text-right">View</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {logs.map((log) => (
              <TableRow
                className="cursor-pointer hover:bg-accent/50"
                key={log.id}
                onClick={() => handleRowClick(log)}
              >
                <TableCell>
                  <div>
                    <div className="font-medium">{log.user_email}</div>
                    {log.user_name && (
                      <div className="text-muted-foreground text-xs">{log.user_name}</div>
                    )}
                  </div>
                </TableCell>
                <TableCell>
                  {(() => {
                    const appearance = getActionTypeBadgeAppearance(log.action_type)
                    return (
                      <Badge className={appearance.className} variant={appearance.variant}>
                        {log.action_type.replace('_', ' ')}
                      </Badge>
                    )
                  })()}
                </TableCell>
                <TableCell className="text-sm">{renderActionCell(log)}</TableCell>
                <TableCell>
                  {(() => {
                    const resourceType =
                      log.resource_type || log.action_type?.split('.')[0] || 'unknown'
                    const appearance = getResourceTypeBadgeAppearance(resourceType)
                    return (
                      <Badge className={appearance.className} variant={appearance.variant}>
                        {resourceType}
                      </Badge>
                    )
                  })()}
                </TableCell>
                <TableCell>
                  <LabelBadge label={resourceLabelForLog(log)} />
                </TableCell>
                <TableCell>
                  {(() => {
                    const appearance = getStatusBadgeAppearance(log.status)
                    const statusLabel = (() => {
                      if (log.status === 'denied') {
                        return 'denied (RBAC)'
                      }
                      if ((log.status === 'failed' || log.status === 'error') && log.http_status) {
                        return `${log.status} (${log.http_status})`
                      }
                      return log.status
                    })()
                    return (
                      <Badge className={appearance.className} variant={appearance.variant}>
                        {statusLabel}
                      </Badge>
                    )
                  })()}
                </TableCell>
                <TableCell className="font-mono text-sm">{log.ip_address || '-'}</TableCell>
                <TableCell className="font-mono text-sm">
                  <TimeAgo date={log.timestamp} />
                </TableCell>
                <TableCell
                  className="text-right"
                  onClick={(event) => {
                    event.stopPropagation()
                    handleRowClick(log)
                  }}
                >
                  <Button size="sm" variant="ghost">
                    <Eye className="h-4 w-4" />
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>

        {totalCount > 0 && (
          <div className="mt-4 flex items-center justify-between">
            <div className="text-muted-foreground text-sm">
              Page {currentPage} of {totalPages}
            </div>
            <div className="flex gap-2">
              <Button disabled={disablePrevious} onClick={onPreviousPage} variant="outline">
                Previous
              </Button>
              <Button disabled={disableNext} onClick={onNextPage} variant="outline">
                Next
              </Button>
            </div>
          </div>
        )}
      </TablePane>

      <Dialog onOpenChange={(open) => !open && setSelected(null)} open={!!selected}>
        <DialogContent className="max-h-[80vh] max-w-2xl overflow-auto">
          <DialogHeader>
            <DialogTitle>Audit Log</DialogTitle>
            <DialogDescription>
              Detailed information for the selected audit log entry:
            </DialogDescription>
          </DialogHeader>
          {selected && (
            <div className="space-y-3 text-sm">
              <div>
                <span className="text-muted-foreground">Timestamp:</span>{' '}
                {new Date(selected.timestamp).toISOString()}
              </div>
              <div>
                <span className="text-muted-foreground">User:</span> {selected.user_email}{' '}
                {selected.user_name ? `(${selected.user_name})` : ''}
              </div>
              <div>
                <span className="text-muted-foreground">Type:</span> {selected.action_type}
              </div>
              <div>
                <span className="text-muted-foreground">Action:</span> {selected.action}
              </div>
              <div>
                <span className="text-muted-foreground">Resource:</span> {selected.resource || '-'}
              </div>
              <div>
                <span className="text-muted-foreground">Resource Type:</span>{' '}
                {selected.resource_type || selected.action_type?.split('.')[0] || 'unknown'}
              </div>
              <div>
                <span className="text-muted-foreground">Status:</span> {(() => {
                  if (selected.status === 'denied') {
                    return 'denied (RBAC)'
                  }
                  if (
                    (selected.status === 'failed' || selected.status === 'error') &&
                    selected.http_status
                  ) {
                    return `${selected.status} (${selected.http_status})`
                  }
                  return selected.status
                })()}
              </div>
              {selected.rbac_decision && (
                <div>
                  <span className="text-muted-foreground">RBAC:</span> {selected.rbac_decision}
                </div>
              )}
              {typeof selected.http_status === 'number' && selected.http_status > 0 && (
                <div>
                  <span className="text-muted-foreground">HTTP Status:</span> {selected.http_status}
                </div>
              )}
              <div>
                <span className="text-muted-foreground">Response Time:</span>{' '}
                {selected.response_time_ms} ms
              </div>
              <div>
                <span className="text-muted-foreground">IP:</span> {selected.ip_address || '-'}
              </div>
              <div className="break-all">
                <span className="text-muted-foreground">User Agent:</span>{' '}
                {selected.user_agent || '-'}
              </div>
              {selected.command && (
                <div className="break-all">
                  <span className="text-muted-foreground">Command:</span>{' '}
                  <code className="rounded border bg-secondary px-1 py-0.5">
                    {selected.command}
                  </code>
                </div>
              )}
              <div className="break-all">
                <span className="text-muted-foreground">Details:</span>
                <pre className="mt-2 max-h-64 overflow-auto rounded bg-muted p-2 text-xs">
                  {(() => {
                    try {
                      return JSON.stringify(JSON.parse(selected.details || '{}'), null, 2)
                    } catch {
                      return selected.details || '-'
                    }
                  })()}
                </pre>
              </div>
              <div className="mt-2 flex justify-end">
                <Button onClick={() => setSelected(null)} variant="outline">
                  Close
                </Button>
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>
    </>
  )
}
