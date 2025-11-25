import { Eye } from 'lucide-react'
import { useMemo, useState } from 'react'
import { renderActionCell } from '@/components/audit-logs/action-cell'
import {
  getActionTypeBadgeAppearance,
  getResourceTypeBadgeAppearance,
  getStatusBadgeAppearance,
  LabelBadge,
} from '@/components/audit-logs/badges'
import { AuditLogDetailDialog } from '@/components/audit-logs/detail-dialog'
import type { AuditLogRecord } from '@/components/audit-logs/types'
import {
  formatStatusLabel,
  getAPITokenInfo,
  resourceLabelForLog,
} from '@/components/audit-logs/utils'
import { TablePane } from '@/components/table-pane'
import { TimeAgo } from '@/components/time-ago'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

export type { AuditLogRecord } from '@/components/audit-logs/types'

type AuditLogsPaneProps = {
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
              <TableHead>Actor</TableHead>
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
            {logs.map((log, index) => {
              const actionType = log.action_type ?? 'unknown'
              const resourceType = log.resource_type ?? actionType.split('.')[0] ?? 'unknown'
              const appearance = getActionTypeBadgeAppearance(actionType)
              const resourceAppearance = getResourceTypeBadgeAppearance(resourceType)
              const statusAppearance = getStatusBadgeAppearance(log.status)
              
              // Handle both standard (timestamp) and aggregated (last_seen/first_seen) logs
              const timestamp = 'timestamp' in log ? log.timestamp : (log as any).last_seen ?? (log as any).first_seen
              const rowKey = log.id ?? `${timestamp ?? 'audit'}-${index}`

              const statusLabel = formatStatusLabel(log)

              const tokenInfo = getAPITokenInfo(log)

              return (
                <TableRow
                  className="cursor-pointer hover:bg-accent/50"
                  key={rowKey}
                  onClick={() => handleRowClick(log)}
                >
                  <TableCell>
                    {tokenInfo.hasToken ? (
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
                    ) : (
                      <div>
                        <div className="font-medium">{log.user_email ?? '-'}</div>
                        {log.user_name && (
                          <div className="text-muted-foreground text-xs">{log.user_name}</div>
                        )}
                      </div>
                    )}
                  </TableCell>
                  <TableCell>
                    <Badge className={appearance.className} variant={appearance.variant}>
                      {actionType.replace('_', ' ')}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-sm">{renderActionCell(log)}</TableCell>
                  <TableCell>
                    <Badge
                      className={resourceAppearance.className}
                      variant={resourceAppearance.variant}
                    >
                      {resourceType}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <LabelBadge label={resourceLabelForLog(log)} />
                  </TableCell>
                  <TableCell>
                    <Badge
                      className={statusAppearance.className}
                      variant={statusAppearance.variant}
                    >
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
                      handleRowClick(log)
                    }}
                  >
                    <Button size="sm" variant="ghost">
                      <Eye className="h-4 w-4" />
                    </Button>
                  </TableCell>
                </TableRow>
              )
            })}
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

      <AuditLogDetailDialog entry={selected} onClose={() => setSelected(null)} />
    </>
  )
}
