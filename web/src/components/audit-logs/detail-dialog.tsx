import { useMemo } from 'react'
import type { AuditLogRecord } from '@/components/audit-logs/types'
import { formatStatusLabel, getAPITokenInfo } from '@/components/audit-logs/utils'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

type AuditLogDetailDialogProps = {
  entry: AuditLogRecord | null
  onClose: () => void
}

export function AuditLogDetailDialog({ entry, onClose }: AuditLogDetailDialogProps) {
  const statusLabel = useMemo(() => (entry ? formatStatusLabel(entry) : '-'), [entry])

  return (
    <Dialog onOpenChange={(open) => !open && onClose()} open={Boolean(entry)}>
      <DialogContent className="max-h-[80vh] max-w-2xl overflow-auto">
        <DialogHeader>
          <DialogTitle>Audit Log</DialogTitle>
          <DialogDescription>
            Detailed information for the selected audit log entry:
          </DialogDescription>
        </DialogHeader>
        {entry ? (
          <div className="space-y-3 text-sm">
            <div>
              <span className="text-muted-foreground">Timestamp:</span>{' '}
              {entry.timestamp ? new Date(entry.timestamp).toISOString() : '-'}
            </div>
            {renderActorDetails(entry)}
            <div>
              <span className="text-muted-foreground">Type:</span> {entry.action_type}
            </div>
            <div>
              <span className="text-muted-foreground">Action:</span> {entry.action}
            </div>
            <div data-testid="audit-event-count">
              <span className="text-muted-foreground">Event Count:</span>{' '}
              {Math.max(1, entry.event_count ?? 1)}
              {(entry.event_count ?? 1) > 1 && (
                <span className="text-muted-foreground"> (aggregated)</span>
              )}
            </div>
            <div>
              <span className="text-muted-foreground">Resource:</span> {entry.resource || '-'}
            </div>
            <div>
              <span className="text-muted-foreground">Resource Type:</span>{' '}
              {entry.resource_type || entry.action_type?.split('.')[0] || 'unknown'}
            </div>
            <div>
              <span className="text-muted-foreground">Status:</span> {statusLabel}
            </div>
            {entry.rbac_decision && (
              <div>
                <span className="text-muted-foreground">RBAC:</span> {entry.rbac_decision}
              </div>
            )}
            {typeof entry.http_status === 'number' && entry.http_status > 0 && (
              <div>
                <span className="text-muted-foreground">HTTP Status:</span> {entry.http_status}
              </div>
            )}
            <div>
              <span className="text-muted-foreground">Response Time:</span>{' '}
              {typeof entry.response_time_ms === 'number' ? `${entry.response_time_ms} ms` : '-'}
            </div>
            <div>
              <span className="text-muted-foreground">IP:</span> {entry.ip_address || '-'}
            </div>
            <div className="break-all">
              <span className="text-muted-foreground">User Agent:</span> {entry.user_agent || '-'}
            </div>
            {entry.command && (
              <div className="break-all">
                <span className="text-muted-foreground">Command:</span>{' '}
                <code className="rounded border bg-secondary px-1 py-0.5">{entry.command}</code>
              </div>
            )}
            <div className="break-all">
              <span className="text-muted-foreground">Details:</span>
              <pre className="mt-2 max-h-64 overflow-auto rounded bg-muted p-2 text-xs">
                {formatDetails(entry.details)}
              </pre>
            </div>
            <div className="mt-2 flex justify-end">
              <Button onClick={onClose} variant="outline">
                Close
              </Button>
            </div>
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  )
}

function renderActorDetails(entry: AuditLogRecord) {
  const tokenInfo = getAPITokenInfo(entry)
  if (tokenInfo.hasToken) {
    return (
      <>
        <div>
          <span className="text-muted-foreground">Token:</span>{' '}
          {tokenInfo.displayName || 'API Token'}
        </div>
        {tokenInfo.tokenId !== null && (
          <div>
            <span className="text-muted-foreground">Token ID:</span> {tokenInfo.tokenId}
          </div>
        )}
        {entry.user_email && (
          <div>
            <span className="text-muted-foreground">Owner:</span> {entry.user_email}{' '}
            {entry.user_name ? `(${entry.user_name})` : ''}
          </div>
        )}
      </>
    )
  }
  return (
    <div>
      <span className="text-muted-foreground">User:</span> {entry.user_email || '-'}{' '}
      {entry.user_name ? `(${entry.user_name})` : ''}
    </div>
  )
}

function formatDetails(details: string | undefined | null): string {
  try {
    return JSON.stringify(JSON.parse(details ?? '{}'), null, 2)
  } catch {
    return details ?? '-'
  }
}
