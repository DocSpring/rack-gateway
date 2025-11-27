import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useCallback, useEffect } from 'react'

import { type AuditLogRecord, AuditLogsPane } from '@/components/audit-logs-pane'
import { api } from '@/lib/api'
import { downloadAuditLogsCsv } from '@/lib/audit-export'
import { AuditFilterPanel } from '@/pages/audit/filter-panel'
import { useAuditMetrics } from '@/pages/audit/metrics'
import { AuditStatsCards } from '@/pages/audit/stats-cards'
import { useAuditSearchParams } from '@/pages/audit/use-audit-search-params'
import { createAuditQueryParams } from '@/pages/audit/utils'

export function AuditPage({ userId, userEmail }: { userId?: string; userEmail?: string } = {}) {
  // Use the new custom hook for search parameters and persistence
  const {
    searchTerm,
    setSearchTerm,
    debouncedSearch,
    actionTypeFilter,
    setActionTypeFilter,
    statusFilter,
    setStatusFilter,
    resourceTypeFilter,
    setResourceTypeFilter,
    dateRange,
    setDateRange,
    perPage,
    setPerPage,
    page,
    setPage,
    customStart,
    customEnd,
    customStartISO,
    customEndISO,
    effectiveUserId,
    resolvedUserEmail,
    handleCustomStartChange,
    handleCustomEndChange,
  } = useAuditSearchParams(userId, userEmail)

  // Fetch audit logs
  const { data, error, isError, isLoading, refetch } = useQuery<
    { logs: AuditLogRecord[]; total: number; page: number; limit: number },
    Error
  >({
    queryKey: [
      'audit-logs',
      actionTypeFilter,
      statusFilter,
      resourceTypeFilter,
      dateRange,
      debouncedSearch,
      effectiveUserId || '',
      resolvedUserEmail,
      page,
      perPage,
      customStartISO || '',
      customEndISO || '',
    ],
    queryFn: () => {
      const params = createAuditQueryParams({
        actionType: actionTypeFilter,
        status: statusFilter,
        resourceType: resourceTypeFilter,
        range: dateRange,
        customStart: customStartISO,
        customEnd: customEndISO,
        search: debouncedSearch,
        userId: effectiveUserId,
        userEmail: resolvedUserEmail,
        page,
        limit: perPage,
        includeDefaultRange: true,
        includeDefaultPagination: true,
      })

      return api.get<{
        logs: AuditLogRecord[]
        total: number
        page: number
        limit: number
      }>(`/api/v1/audit-logs?${params}`)
    },
    placeholderData: keepPreviousData,
    refetchOnMount: 'always',
    refetchOnWindowFocus: true,
    staleTime: 0,
  })

  const logs: AuditLogRecord[] = data?.logs ?? []
  const totalCount = data?.total ?? logs.length

  const { stats, pagination } = useAuditMetrics({
    logs,
    perPage,
    totalCount,
    page,
  })
  const { totalPages, currentPage, firstRowIndex, lastRowIndex } = pagination
  const {
    total: totalEvents,
    success: successEvents,
    failed: failedEvents,
    denied: deniedEvents,
    avgResponseTime,
  } = stats

  useEffect(() => {
    if (!data) {
      return
    }

    if (page > totalPages) {
      setPage(totalPages)
    }
  }, [data, page, totalPages, setPage])

  const handleExport = useCallback(() => {
    downloadAuditLogsCsv({
      actionType: actionTypeFilter,
      status: statusFilter,
      resourceType: resourceTypeFilter,
      range: dateRange,
      customStart: customStartISO,
      customEnd: customEndISO,
      search: searchTerm,
      userId: effectiveUserId,
      userEmail: resolvedUserEmail,
      includeDefaultRange: true,
    })
  }, [
    actionTypeFilter,
    customEndISO,
    customStartISO,
    dateRange,
    effectiveUserId,
    resolvedUserEmail,
    searchTerm,
    statusFilter,
    resourceTypeFilter,
  ])

  // Do not unmount the page on loading/error; render inline status instead to preserve input focus

  const titleEmail = resolvedUserEmail || userEmail
  const title = titleEmail ? `Audit Logs: ${titleEmail}` : 'Audit Logs'

  const handleActionTypeChange = useCallback(
    (value: string) => {
      setActionTypeFilter(value)
      setPage(1)
    },
    [setActionTypeFilter, setPage]
  )

  const handleDateRangeChange = useCallback(
    (value: string) => {
      setDateRange(value)
      setPage(1)
    },
    [setDateRange, setPage]
  )

  const handlePerPageChange = useCallback(
    (value: number) => {
      setPerPage(value)
      setPage(1)
    },
    [setPerPage, setPage]
  )

  const handleResourceTypeChange = useCallback(
    (value: string) => {
      setResourceTypeFilter(value)
      setPage(1)
    },
    [setResourceTypeFilter, setPage]
  )

  const handleStatusChange = useCallback(
    (value: string) => {
      setStatusFilter(value)
      setPage(1)
    },
    [setStatusFilter, setPage]
  )

  const handleNextPage = useCallback(() => {
    setPage((p) => Math.min(totalPages, p + 1))
  }, [totalPages, setPage])

  const handlePreviousPage = useCallback(() => {
    setPage((p) => Math.max(1, p - 1))
  }, [setPage])

  return (
    <div className="p-8">
      <div className="mb-8">
        <h1 className="font-bold text-3xl">{title}</h1>
        <p className="mt-2 text-muted-foreground">
          Monitor all gateway activity and access patterns
        </p>
      </div>

      <AuditStatsCards
        averageResponseTimeMs={avgResponseTime}
        deniedEvents={deniedEvents}
        failedEvents={failedEvents}
        successfulEvents={successEvents}
        totalCount={totalCount}
        totalEvents={totalEvents}
      />

      <AuditFilterPanel
        actionType={actionTypeFilter}
        customEnd={customEnd}
        customStart={customStart}
        dateRange={dateRange}
        disableExport={logs.length === 0}
        disableRefresh={isLoading}
        isCustomRange={dateRange === 'custom'}
        onActionTypeChange={handleActionTypeChange}
        onCustomEndChange={handleCustomEndChange}
        onCustomStartChange={handleCustomStartChange}
        onDateRangeChange={handleDateRangeChange}
        onExport={handleExport}
        onPerPageChange={handlePerPageChange}
        onRefresh={() => refetch()}
        onResourceTypeChange={handleResourceTypeChange}
        onSearchChange={setSearchTerm}
        onStatusChange={handleStatusChange}
        perPage={perPage}
        resourceType={resourceTypeFilter}
        searchTerm={searchTerm}
        status={statusFilter}
      />

      <AuditLogsPane
        currentPage={currentPage}
        disableNext={currentPage >= totalPages}
        disablePrevious={currentPage <= 1}
        error={
          isError
            ? `Failed to load audit logs: ${String((error as Error)?.message || 'Unknown error')}`
            : null
        }
        firstRowIndex={firstRowIndex}
        lastRowIndex={lastRowIndex}
        loading={Boolean(isLoading && logs.length === 0)}
        logs={logs}
        onNextPage={handleNextPage}
        onPreviousPage={handlePreviousPage}
        title={title}
        totalCount={totalCount}
        totalPages={totalPages}
      />
    </div>
  )
}
