import type { ReactElement } from 'react'
import type { AuditLogRecord } from '@/components/audit-logs/types'
import { extractExecCommand } from '@/components/audit-logs/utils'
import { Badge } from '@/components/ui/badge'

export function renderActionCell(log: AuditLogRecord): ReactElement {
  const eventCount = Math.max(1, log.event_count ?? 1)
  const countBadge =
    eventCount > 1 ? (
      <Badge
        className="w-fit border border-gray-300 bg-transparent font-mono text-gray-300"
        variant="outline"
      >
        {`×${eventCount}`}
      </Badge>
    ) : null

  if (log.action_type === 'convox' && log.action === 'process.exec') {
    const command = extractExecCommand(log)
    const truncated = command.length > 64 ? `${command.slice(0, 64)}…` : command
    return (
      <div className="flex flex-col">
        <div className="flex items-center gap-2">
          <Badge
            className="w-fit border border-border bg-muted font-mono text-muted-foreground"
            variant="outline"
          >
            {log.action}
          </Badge>
          {countBadge}
        </div>
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
    <div className="flex items-center gap-2">
      <Badge
        className="border border-border bg-muted font-mono text-muted-foreground"
        variant="outline"
      >
        {log.action ?? '-'}
      </Badge>
      {countBadge}
    </div>
  )
}
