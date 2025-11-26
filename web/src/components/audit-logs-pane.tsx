import { useMemo, useState } from 'react'
import { AuditLogRow } from '@/components/audit-logs/audit-log-row'
import { AuditLogDetailDialog } from '@/components/audit-logs/detail-dialog'
import type { AuditLogRecord } from '@/components/audit-logs/types'
import { TablePane } from '@/components/table-pane'
import { Button } from '@/components/ui/button'
import { Table, TableBody, TableHead, TableHeader, TableRow } from '@/components/ui/table'

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
            {logs.map((log, index) => (
              <AuditLogRow
                index={index}
                key={log.id ?? `${index}`}
                log={log}
                onClick={handleRowClick}
              />
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

      <AuditLogDetailDialog entry={selected} onClose={() => setSelected(null)} />
    </>
  )
}
