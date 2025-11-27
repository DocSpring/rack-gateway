import { format } from 'date-fns'

import { createAuditQueryParams } from '@/pages/audit/utils'

function formatAuditExportFileName() {
  return `audit-logs-${format(new Date(), 'yyyy-MM-dd')}.csv`
}

export function downloadAuditLogsCsv(options: {
  actionType: string
  status: string
  resourceType: string
  range: string
  customStart?: string
  customEnd?: string
  search?: string
  userId?: string
  userEmail?: string
  includeDefaultRange?: boolean
}) {
  const params = createAuditQueryParams({
    ...options,
    includeDefaultRange: true,
  })
  params.append('format', 'csv')

  // Create download link
  const url = `/api/v1/audit-logs/export?${params}`
  const link = document.createElement('a')
  link.href = url
  link.download = formatAuditExportFileName()
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
}
