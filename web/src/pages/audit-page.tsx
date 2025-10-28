import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import {
  AuditLogsPane,
  type AuditLogRecord,
} from '@/components/audit-logs-pane'
import { api } from '@/lib/api'
import { DEFAULT_PER_PAGE } from '@/lib/constants'
import { AuditFilterPanel } from '@/pages/audit/filter-panel'
import { useAuditMetrics } from '@/pages/audit/metrics'
import { AuditStatsCards } from '@/pages/audit/stats-cards'
import {
	ACTION_TYPES,
	DEFAULT_DATE_RANGE,
	RESOURCE_TYPES,
	STATUS_TYPES,
	VALID_DATE_RANGES,
	createAuditQueryParams,
	ensureFilterValue,
	formatAuditExportFileName,
	isValidDateTime,
	toDateTimeLocalInput,
	toISOStringParam,
} from '@/pages/audit/utils'

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: acceptable complexity for this page component
export function AuditPage({ userId, userEmail }: { userId?: string; userEmail?: string } = {}) {
  const initialSearchParams =
    typeof window !== 'undefined' ? new URLSearchParams(window.location.search) : undefined
  const initialRangeParam = initialSearchParams?.get('range')
  const queryUserEmail = initialSearchParams?.get('user')?.trim() ?? ''
  const resolvedUserEmail = userEmail?.trim() || queryUserEmail

  const [searchTerm, setSearchTerm] = useState(
    () => initialSearchParams?.get('search') ?? resolvedUserEmail
  )
  const [debouncedSearch, setDebouncedSearch] = useState(searchTerm)
  const [actionTypeFilter, setActionTypeFilter] = useState(() =>
    ensureFilterValue(initialSearchParams?.get('action_type'), ACTION_TYPES)
  )
  const [statusFilter, setStatusFilter] = useState(() =>
    ensureFilterValue(initialSearchParams?.get('status'), STATUS_TYPES)
  )
  const [resourceTypeFilter, setResourceTypeFilter] = useState(() =>
    ensureFilterValue(initialSearchParams?.get('resource_type'), RESOURCE_TYPES)
  )
  const [dateRange, setDateRange] = useState(() => {
    const param = initialRangeParam
    if (param && (VALID_DATE_RANGES.has(param) || param === 'custom')) {
      return param
    }
    if (initialSearchParams?.has('start') || initialSearchParams?.has('end')) {
      return 'custom'
    }
    try {
      const stored = localStorage.getItem('audit_date_range')
      if (stored && (VALID_DATE_RANGES.has(stored) || stored === 'custom')) {
        return stored
      }
    } catch {
      /* ignore */
    }
    return DEFAULT_DATE_RANGE
  })
  const [perPage, setPerPage] = useState<number>(() => {
    const param = initialSearchParams?.get('limit')
    if (param) {
      const parsed = Number.parseInt(param, 10)
      if (Number.isFinite(parsed) && parsed > 0) {
        return parsed
      }
    }
    try {
      const v = localStorage.getItem('audit_per_page')
      return v ? Math.max(1, Number.parseInt(v, 10)) : DEFAULT_PER_PAGE
    } catch {
      return DEFAULT_PER_PAGE
    }
  })
  const [page, setPage] = useState(() => {
    const param = initialSearchParams?.get('page')
    if (param) {
      const parsed = Number.parseInt(param, 10)
      if (Number.isFinite(parsed) && parsed > 0) {
        return parsed
      }
    }
    return 1
  })
  const [queryUserId] = useState(() => initialSearchParams?.get('user_id') ?? '')
  const effectiveUserId = userId ?? (queryUserId || undefined)
  const searchInitializedRef = useRef(false)
  const [customStart, setCustomStart] = useState(() => {
    const fromQuery = initialSearchParams?.get('start')
    if (fromQuery) {
      return toDateTimeLocalInput(fromQuery)
    }
    if (
      typeof window !== 'undefined' &&
      (initialRangeParam === 'custom' || dateRange === 'custom')
    ) {
      try {
        const stored = localStorage.getItem('audit_custom_start')
        if (stored) {
          return stored
        }
      } catch {
        /* ignore */
      }
    }
    return ''
  })
  const [customEnd, setCustomEnd] = useState(() => {
    const fromQuery = initialSearchParams?.get('end')
    if (fromQuery) {
      return toDateTimeLocalInput(fromQuery)
    }
    if (
      typeof window !== 'undefined' &&
      (initialRangeParam === 'custom' || dateRange === 'custom')
    ) {
      try {
        const stored = localStorage.getItem('audit_custom_end')
        if (stored) {
          return stored
        }
      } catch {
        /* ignore */
      }
    }
    return ''
  })

  const customStartISO = useMemo(() => {
    if (dateRange !== 'custom') {
      return
    }
    return toISOStringParam(customStart)
  }, [customStart, dateRange])

  const customEndISO = useMemo(() => {
    if (dateRange !== 'custom') {
      return
    }
    return toISOStringParam(customEnd)
  }, [customEnd, dateRange])

  const handleCustomStartChange = useCallback((next: string) => {
    setCustomStart(next)
    setPage(1)
    if (!isValidDateTime(next)) {
      return
    }
    setCustomEnd((prev) => {
      if (!(prev && isValidDateTime(prev))) {
        return next
      }
      const prevTime = new Date(prev).getTime()
      const nextTime = new Date(next).getTime()
      return prevTime < nextTime ? next : prev
    })
  }, [])

  const handleCustomEndChange = useCallback((next: string) => {
    setCustomEnd(next)
    setPage(1)
    if (!isValidDateTime(next)) {
      return
    }
    setCustomStart((prev) => {
      if (!(prev && isValidDateTime(prev))) {
        return next
      }
      const prevTime = new Date(prev).getTime()
      const nextTime = new Date(next).getTime()
      return prevTime > nextTime ? next : prev
    })
  }, [])

  // Persist selected date range and per-page to localStorage
  useEffect(() => {
    try {
      localStorage.setItem('audit_date_range', dateRange)
    } catch (_e) {
      /* ignore */
    }
  }, [dateRange])
  useEffect(() => {
    try {
      localStorage.setItem('audit_per_page', String(perPage))
    } catch (_e) {
      /* ignore */
    }
  }, [perPage])

  useEffect(() => {
    if (dateRange !== 'custom') {
      return
    }
    try {
      if (customStart) {
        localStorage.setItem('audit_custom_start', customStart)
      } else {
        localStorage.removeItem('audit_custom_start')
      }
    } catch (_e) {
      /* ignore */
    }
  }, [customStart, dateRange])

  useEffect(() => {
    if (dateRange !== 'custom') {
      return
    }
    try {
      if (customEnd) {
        localStorage.setItem('audit_custom_end', customEnd)
      } else {
        localStorage.removeItem('audit_custom_end')
      }
    } catch (_e) {
      /* ignore */
    }
  }, [customEnd, dateRange])

  useEffect(() => {
    if (dateRange !== 'custom') {
      return
    }
    if (customStart || customEnd) {
      return
    }
    try {
      const storedStart = localStorage.getItem('audit_custom_start')
      const storedEnd = localStorage.getItem('audit_custom_end')
      if (storedStart) {
        setCustomStart(storedStart)
      }
      if (storedEnd) {
        setCustomEnd(storedEnd)
      }
    } catch (_e) {
      /* ignore */
    }
  }, [customStart, customEnd, dateRange])

  // Debounce search to prevent refetch on every keystroke
  useEffect(() => {
    if (!searchInitializedRef.current) {
      searchInitializedRef.current = true
      setDebouncedSearch(searchTerm)
      return
    }

    setPage(1)
    const timer = setTimeout(() => setDebouncedSearch(searchTerm), 300)
    return () => clearTimeout(timer)
  }, [searchTerm])

  const routingQuery = useMemo(
    () =>
      createAuditQueryParams({
        actionType: actionTypeFilter,
        status: statusFilter,
        resourceType: resourceTypeFilter,
        range: dateRange,
        customStart: customStartISO,
        customEnd: customEndISO,
        search: searchTerm,
        userId: effectiveUserId,
        userEmail: resolvedUserEmail,
        page,
        limit: perPage,
      }).toString(),
    [
      actionTypeFilter,
      customEndISO,
      customStartISO,
      dateRange,
      effectiveUserId,
      resolvedUserEmail,
      page,
      perPage,
      resourceTypeFilter,
      searchTerm,
      statusFilter,
    ]
  )

  useEffect(() => {
    if (typeof window === 'undefined') {
      return
    }

    const currentQuery = window.location.search.startsWith('?')
      ? window.location.search.slice(1)
      : window.location.search

    if (currentQuery === routingQuery) {
      return
    }

    const newUrl = `${window.location.pathname}${routingQuery ? `?${routingQuery}` : ''}${
      window.location.hash
    }`
    window.history.replaceState(null, '', newUrl)
  }, [routingQuery])

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
  }, [data, page, totalPages])

  const handleExport = () => {
    const params = createAuditQueryParams({
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

  // Do not unmount the page on loading/error; render inline status instead to preserve input focus

  const titleEmail = resolvedUserEmail || userEmail
  const title = titleEmail ? `Audit Logs: ${titleEmail}` : 'Audit Logs'

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
        onActionTypeChange={(value) => {
          setActionTypeFilter(value)
          setPage(1)
        }}
        onCustomEndChange={handleCustomEndChange}
        onCustomStartChange={handleCustomStartChange}
        onDateRangeChange={(value) => {
          setDateRange(value)
          setPage(1)
        }}
        onExport={handleExport}
        onPerPageChange={(value) => {
          setPerPage(value)
          setPage(1)
        }}
        onRefresh={() => refetch()}
        onResourceTypeChange={(value) => {
          setResourceTypeFilter(value)
          setPage(1)
        }}
        onSearchChange={setSearchTerm}
        onStatusChange={(value) => {
          setStatusFilter(value)
          setPage(1)
        }}
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
        loading={!!(isLoading && logs.length === 0)}
        logs={logs}
        onNextPage={() => setPage((p) => Math.min(totalPages, p + 1))}
        onPreviousPage={() => setPage((p) => Math.max(1, p - 1))}
        title={title}
        totalCount={totalCount}
        totalPages={totalPages}
      />
    </div>
  )
}
