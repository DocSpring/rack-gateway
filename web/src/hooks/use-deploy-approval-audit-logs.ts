import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useCallback, useState } from 'react'

import type { AuditLogRecord } from '@/components/audit-logs-pane'
import { type AuditLogsResponse, api } from '@/lib/api'
import { DEFAULT_PER_PAGE } from '@/lib/constants'

export function useDeployApprovalAuditLogs(id: string, enabled: boolean) {
  const [pageIndex, setPageIndex] = useState(1)

  const { data, isLoading, error } = useQuery<AuditLogsResponse, Error>({
    queryKey: ['deployApprovalRequestAuditLogs', id, pageIndex, DEFAULT_PER_PAGE],
    queryFn: () =>
      api.get(
        `/api/v1/deploy-approval-requests/${id}/audit-logs?limit=${DEFAULT_PER_PAGE}&page=${pageIndex}`
      ),
    enabled,
    placeholderData: keepPreviousData,
    refetchOnWindowFocus: true,
    staleTime: 0,
  })

  const logs = (data?.logs ?? []) as AuditLogRecord[]
  const total = data?.total ?? 0
  const limit = data?.limit ?? DEFAULT_PER_PAGE
  const currentPage = data?.page ?? pageIndex
  const totalPages = Math.max(1, Math.ceil(Math.max(total, 0) / limit))
  const firstRowIndex = total === 0 ? 0 : (currentPage - 1) * limit + 1
  const lastRowIndex = total === 0 ? 0 : firstRowIndex + logs.length - 1

  const goToPreviousPage = useCallback(() => {
    setPageIndex((prev) => Math.max(1, prev - 1))
  }, [])

  const goToNextPage = useCallback(() => {
    setPageIndex((prev) => Math.min(totalPages, prev + 1))
  }, [totalPages])

  return {
    auditLogs: logs,
    auditTotal: total,
    auditTotalPages: totalPages,
    auditFirstRowIndex: firstRowIndex,
    auditLastRowIndex: lastRowIndex,
    auditLoading: isLoading,
    auditError: error,
    auditPage: currentPage,
    goToPreviousPage,
    goToNextPage,
  }
}
