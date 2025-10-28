import type { AuditLogRecord } from '@/components/audit-logs-pane'
import { AuditLogsPane } from '@/components/audit-logs-pane'

type UserAuditLogsProps = {
  logs: AuditLogRecord[]
  totalCount: number
  totalPages: number
  firstRowIndex: number
  lastRowIndex: number
  loading: boolean
  error: string | null
  currentPage: number
  onPreviousPage: () => void
  onNextPage: () => void
}

export function UserAuditLogsSection({
  logs,
  totalCount,
  totalPages,
  firstRowIndex,
  lastRowIndex,
  loading,
  error,
  currentPage,
  onPreviousPage,
  onNextPage,
}: UserAuditLogsProps) {
  return (
    <div data-testid="user-audit-logs">
      <AuditLogsPane
        currentPage={currentPage}
        disableNext={currentPage >= totalPages}
        disablePrevious={currentPage <= 1}
        emptyMessage="No audit logs for this user"
        error={error}
        firstRowIndex={firstRowIndex}
        lastRowIndex={lastRowIndex}
        loading={loading}
        logs={logs}
        onNextPage={onNextPage}
        onPreviousPage={onPreviousPage}
        title="Audit Logs"
        totalCount={totalCount}
        totalPages={totalPages}
      />
    </div>
  )
}
