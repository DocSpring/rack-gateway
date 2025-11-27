import { Eye } from 'lucide-react'
import { renderActionCell } from '@/components/audit-logs/action-cell'
import {
  getActionTypeBadgeAppearance,
  getResourceTypeBadgeAppearance,
  getStatusBadgeAppearance,
  LabelBadge,
} from '@/components/audit-logs/badges'
import type { AuditLogRecord } from '@/components/audit-logs/types'
import {
  formatStatusLabel,
  getAPITokenInfo,
  resourceLabelForLog,
} from '@/components/audit-logs/utils'
import { TimeAgo } from '@/components/time-ago'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { TableCell, TableRow } from '@/components/ui/table'

type AuditLogRowProps = {
  log: AuditLogRecord
  index: number
  onClick: (log: AuditLogRecord) => void
}

export function AuditLogRow({ log, index, onClick }: AuditLogRowProps) {
  const actionType = log.action_type ?? 'unknown'
  const resourceType = log.resource_type ?? actionType.split('.')[0] ?? 'unknown'
  const appearance = getActionTypeBadgeAppearance(actionType)
  const resourceAppearance = getResourceTypeBadgeAppearance(resourceType)
  const statusAppearance = getStatusBadgeAppearance(log.status)

  // Handle both standard (timestamp) and aggregated (last_seen/first_seen) logs
  const timestamp =
    ('timestamp' in log ? log.timestamp : undefined) ??
    ('last_seen' in log ? log.last_seen : undefined) ??
    ('first_seen' in log ? log.first_seen : undefined)
  const rowKey = log.id ?? `${timestamp ?? 'audit'}-${index}`

  const statusLabel = formatStatusLabel(log)

  return (
    <TableRow
      className="cursor-pointer hover:bg-accent/50"
      key={rowKey}
      onClick={() => onClick(log)}
    >
      <TableCell>
        <ActorCell log={log} />
      </TableCell>
      <TableCell>
        <Badge className={appearance.className} variant={appearance.variant}>
          {actionType.replaceAll('_', ' ')}
        </Badge>
      </TableCell>
      <TableCell className="text-sm">{renderActionCell(log)}</TableCell>
      <TableCell>
        <Badge className={resourceAppearance.className} variant={resourceAppearance.variant}>
          {resourceType}
        </Badge>
      </TableCell>
      <TableCell>
        <LabelBadge label={resourceLabelForLog(log)} />
      </TableCell>
      <TableCell>
        <Badge className={statusAppearance.className} variant={statusAppearance.variant}>
          {statusLabel}
        </Badge>
      </TableCell>
      <TableCell className="font-mono text-sm">{log.ip_address || '-'}</TableCell>
      <TableCell className="font-mono text-sm">
        <TimeAgo date={timestamp ?? null} />
      </TableCell>
      <TableCell
        className="text-right"
        onClick={(event) => {
          event.stopPropagation()
          onClick(log)
        }}
      >
        <Button size="sm" variant="ghost">
          <Eye className="h-4 w-4" />
        </Button>
      </TableCell>
    </TableRow>
  )
}

function ActorCell({ log }: { log: AuditLogRecord }) {
  const tokenInfo = getAPITokenInfo(log)
  if (tokenInfo.hasToken) {
    return (
      <div>
        <div className="font-semibold text-[11px] text-muted-foreground uppercase tracking-wide">
          API Token
        </div>
        <div className="font-medium">{tokenInfo.displayName || 'API Token'}</div>
        {log.user_email && (
          <div className="text-muted-foreground text-xs">
            Owner: {log.user_email}
            {log.user_name ? ` (${log.user_name})` : ''}
          </div>
        )}
      </div>
    )
  }
  return (
    <div>
      <div className="font-medium">{log.user_email ?? '-'}</div>
      {log.user_name && <div className="text-muted-foreground text-xs">{log.user_name}</div>}
    </div>
  )
}
