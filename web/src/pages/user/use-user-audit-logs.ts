import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'
import type { AuditLogRecord } from '@/components/audit-logs-pane'
import type { AuditLogsResponse } from '@/lib/api'
import { api } from '@/lib/api'
import { DEFAULT_PER_PAGE } from '@/lib/constants'
import { QUERY_KEYS } from '@/lib/query-keys'

type UserAuditLogsState = {
  logs: AuditLogRecord[]
  totalCount: number
  currentPage: number
  totalPages: number
  firstRowIndex: number
  lastRowIndex: number
  loading: boolean
  error: string | null
  limit: number
  goToPreviousPage: () => void
  goToNextPage: () => void
}

export function useUserAuditLogs(email: string | null, enabled: boolean): UserAuditLogsState {
  const [pageIndex, setPageIndex] = useState(1)

  const { data, isLoading, error } = useQuery<AuditLogsResponse, Error>({
    queryKey: [...QUERY_KEYS.USER_AUDIT_LOGS, email, pageIndex, DEFAULT_PER_PAGE],
    queryFn: () =>
      email
        ? api.listAuditLogs({ user: email, page: pageIndex, limit: DEFAULT_PER_PAGE, range: '30d' })
        : Promise.reject(new Error('Missing email parameter')),
    enabled: Boolean(email) && enabled,
    placeholderData: keepPreviousData,
    refetchOnWindowFocus: true,
    staleTime: 0,
  })

  const auditLogs = (data?.logs ?? []) as AuditLogRecord[]
  const total = data?.total ?? 0
  const limit = data?.limit ?? DEFAULT_PER_PAGE
  const currentPage = data?.page ?? pageIndex
  const totalPages = Math.max(1, Math.ceil(Math.max(total, 0) / limit))
  const firstRowIndex = total === 0 ? 0 : (currentPage - 1) * limit + 1
  const lastRowIndex = total === 0 ? 0 : firstRowIndex + auditLogs.length - 1
  const loading = isLoading && auditLogs.length === 0
  const errorMessage = error ? error.message : null

  useEffect(() => {
    if (!data) {
      return
    }
    if (pageIndex !== currentPage) {
      setPageIndex(currentPage)
      return
    }
    if (currentPage > totalPages) {
      setPageIndex(totalPages)
    }
  }, [data, pageIndex, currentPage, totalPages])

  const goToPreviousPage = useMemo(() => () => setPageIndex((prev) => Math.max(1, prev - 1)), [])
  const goToNextPage = useMemo(
    () => () => setPageIndex((prev) => Math.min(totalPages, prev + 1)),
    [totalPages]
  )

  return {
    logs: auditLogs,
    totalCount: total,
    currentPage,
    totalPages,
    firstRowIndex,
    lastRowIndex,
    loading,
    error: errorMessage,
    limit,
    goToPreviousPage,
    goToNextPage,
  }
}
