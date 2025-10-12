import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { format } from 'date-fns'
import { Calendar as CalendarIcon, Download, RefreshCw, Search } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Calendar } from '@/components/ui/calendar'
import { NativeSelect } from '@/components/ui/native-select'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { type AuditLogRecord, AuditLogsPane } from '../components/audit-logs-pane'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/card'
import { Input } from '../components/ui/input'
import { Label } from '../components/ui/label'
import { api } from '../lib/api'
import { DEFAULT_PER_PAGE } from '../lib/constants'

const RESOURCE_TYPES = {
  all: 'All Resources',
  app: 'App',
  rack: 'Rack',
  env: 'Env',
  secret: 'Secret',
  process: 'Process',
  system: 'System',
  api_token: 'API Token',
  user: 'User',
  auth: 'Auth',
  admin: 'Admin',
}
const ACTION_TYPES = {
  all: 'All Actions',
  convox: 'Convox API',
  auth: 'Authentication',
  users: 'User Management',
  tokens: 'Token Management',
}

const STATUS_TYPES = {
  all: 'All Statuses',
  success: 'Success',
  failed: 'Failed',
  blocked: 'Blocked',
}

const VALID_DATE_RANGES = new Set(['15m', '1h', '24h', '7d', '30d', 'all', 'custom'])
const DEFAULT_DATE_RANGE = '7d'

type AuditQueryParamsOptions = {
  actionType: string
  status: string
  resourceType: string
  range: string
  customStart?: string
  customEnd?: string
  search?: string
  userId?: string
  userEmail?: string
  page?: number
  limit?: number
  includeDefaultRange?: boolean
  includeDefaultPagination?: boolean
}

function createAuditQueryParams(options: AuditQueryParamsOptions): URLSearchParams {
  const params = new URLSearchParams()
  const {
    actionType,
    status,
    resourceType,
    range,
    customStart,
    customEnd,
    search,
    userId,
    userEmail,
    page,
    limit,
    includeDefaultRange = false,
    includeDefaultPagination = false,
  } = options

  appendSimpleFilters(params, [
    ['action_type', actionType, 'all'],
    ['status', status, 'all'],
    ['resource_type', resourceType, 'all'],
  ])

  appendRangeFilters(params, {
    range,
    customStart,
    customEnd,
    includeDefaultRange,
  })

  if (search) {
    params.set('search', search)
  }

  if (userEmail) {
    params.set('user', userEmail)
  }

  if (userId) {
    params.set('user_id', userId)
  }

  appendPagination(params, {
    page,
    limit,
    includeDefaultPagination,
  })

  return params
}

function appendSimpleFilters(
  params: URLSearchParams,
  entries: [string, string | undefined, string][]
) {
  for (const [key, value, skip] of entries) {
    if (value && value !== skip) {
      params.set(key, value)
    }
  }
}

function appendRangeFilters(
  params: URLSearchParams,
  options: {
    range: string
    customStart?: string
    customEnd?: string
    includeDefaultRange: boolean
  }
) {
  const { range, customStart, customEnd, includeDefaultRange } = options
  if (!range) {
    return
  }

  if (includeDefaultRange || range !== DEFAULT_DATE_RANGE) {
    params.set('range', range)
  }

  if (range === 'custom') {
    if (customStart) {
      params.set('start', customStart)
    }
    if (customEnd) {
      params.set('end', customEnd)
    }
  }
}

function appendPagination(
  params: URLSearchParams,
  options: {
    page?: number
    limit?: number
    includeDefaultPagination: boolean
  }
) {
  const { page, limit, includeDefaultPagination } = options

  const shouldIncludePage =
    typeof page === 'number' && page > 0 && (includeDefaultPagination || page > 1)
  if (shouldIncludePage) {
    params.set('page', String(page))
  }

  const shouldIncludeLimit =
    typeof limit === 'number' &&
    limit > 0 &&
    (includeDefaultPagination || limit !== DEFAULT_PER_PAGE)
  if (shouldIncludeLimit) {
    params.set('limit', String(limit))
  }
}

const DATE_ONLY_FORMAT = 'yyyy-MM-dd'
const DATE_SPLIT_PATTERN = /[T\s]/
const TIME_PATTERN = /^[0-9]{2}:[0-9]{2}$/

function toDateTimeLocalInput(value?: string | null): string {
  if (!value) {
    return ''
  }

  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value.length >= 16 ? value.slice(0, 16) : ''
  }

  const offsetMinutes = date.getTimezoneOffset()
  const local = new Date(date.getTime() - offsetMinutes * 60_000)
  return local.toISOString().slice(0, 16)
}

function parseLocalDateTime(value: string | undefined) {
  if (!value) {
    return null
  }

  const { datePart, timePart } = splitDateTime(value)
  if (!datePart) {
    return null
  }

  const [year, month, day] = datePart.split('-').map(Number)
  if ([year, month, day].some(Number.isNaN)) {
    return null
  }

  const safeTime = TIME_PATTERN.test(timePart) ? timePart : '00:00'
  const [hour, minute] = safeTime.split(':').map(Number)
  if ([hour, minute].some(Number.isNaN)) {
    return null
  }

  const local = new Date(year, month - 1, day, hour, minute, 0, 0)
  if (Number.isNaN(local.getTime())) {
    return null
  }

  return local
}

function toISOStringParam(value?: string | null): string | undefined {
  if (!value) {
    return
  }

  const parsed = parseLocalDateTime(value)
  return parsed?.toISOString()
}

function splitDateTime(value: string | undefined) {
  if (!value) {
    return { datePart: '', timePart: '' }
  }

  const trimmed = value.trim()
  const [rawDate = '', rawTime = ''] = trimmed.split('T')
  const datePart = rawDate.split(' ')[0] || ''
  const timeCandidate = rawTime || trimmed.split(' ')[1] || ''
  const truncatedTime = timeCandidate.slice(0, 5)

  return {
    datePart,
    timePart: TIME_PATTERN.test(truncatedTime) ? truncatedTime : '',
  }
}

function combineDateTime(datePart: string, timePart: string) {
  const normalizedDate = datePart.trim().split(DATE_SPLIT_PATTERN)[0] || ''
  if (!normalizedDate) {
    return ''
  }
  const normalizedTime = timePart.trim().slice(0, 5)
  const safeTime = TIME_PATTERN.test(normalizedTime) ? normalizedTime : '00:00'
  return `${normalizedDate}T${safeTime}`
}

function parseDateTime(value: string | undefined) {
  return parseLocalDateTime(value)
}

function isValidDateTime(value: string | undefined) {
  return parseDateTime(value) !== null
}

const ensureFilterValue = (value: string | null | undefined, options: Record<string, string>) =>
  value && value in options ? value : 'all'

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
      }>(`/api/v1/admin/audit?${params}`)
    },
    placeholderData: keepPreviousData,
    refetchOnMount: 'always',
    refetchOnWindowFocus: true,
    staleTime: 0,
  })

  const logs: AuditLogRecord[] = data?.logs ?? []
  const totalCount = data?.total ?? logs.length

  useEffect(() => {
    if (!data) {
      return
    }

    const maxPage = Math.max(1, Math.ceil(Math.max(totalCount, 0) / perPage))
    if (page > maxPage) {
      setPage(maxPage)
    }
  }, [data, page, perPage, totalCount])

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
    const url = `/api/v1/admin/audit/export?${params}`
    const link = document.createElement('a')
    link.href = url
    link.download = `audit-logs-${format(new Date(), 'yyyy-MM-dd')}.csv`
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
  }

  // Do not unmount the page on loading/error; render inline status instead to preserve input focus

  // Calculate statistics
  const countEvents = (predicate?: (log: AuditLogRecord) => boolean) =>
    logs.reduce((acc, log) => {
      const occurrences = Math.max(1, log.event_count ?? 1)
      if (!predicate || predicate(log)) {
        return acc + occurrences
      }
      return acc
    }, 0)

  const totalEvents = countEvents()
  const successEvents = countEvents((l) => l.status === 'success')
  const failedEvents = countEvents((l) => l.status === 'failed')
  const deniedEvents = countEvents((l) => l.status === 'denied' || l.status === 'blocked')
  const totalResponseTime = logs.reduce((acc, l) => {
    if (typeof l.response_time_ms !== 'number') {
      return acc
    }
    return acc + l.response_time_ms * Math.max(1, l.event_count ?? 1)
  }, 0)

  const pageStats = {
    total: totalEvents,
    success: successEvents,
    failed: failedEvents,
    denied: deniedEvents,
    avgResponseTime: totalEvents > 0 ? Math.round(totalResponseTime / totalEvents) : 0,
  }

  const totalPages = Math.max(1, Math.ceil(Math.max(totalCount, 1) / perPage))
  const currentPage = Math.min(page, totalPages)
  const pageItems = logs
  const firstRowIndex = totalCount === 0 ? 0 : (currentPage - 1) * perPage + 1
  const lastRowIndex = totalCount === 0 ? 0 : firstRowIndex + logs.length - 1

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

      {/* Statistics Cards */}
      <div className="mb-6 grid gap-6 md:grid-cols-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="font-medium text-muted-foreground text-sm">Total Logs</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="font-bold text-2xl">{totalCount}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="font-medium text-muted-foreground text-sm">
              Success Rate
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="font-bold text-2xl text-green-600">
              {pageStats.total > 0 ? Math.round((pageStats.success / pageStats.total) * 100) : 0}%
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="font-medium text-muted-foreground text-sm">
              Failed/Denied
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="font-bold text-2xl text-red-600">
              {pageStats.failed + pageStats.denied}
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="font-medium text-muted-foreground text-sm">
              Avg Response Time
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="font-bold text-2xl">{pageStats.avgResponseTime}ms</div>
          </CardContent>
        </Card>
      </div>

      {/* Filters */}
      <Card className="mb-6">
        <CardHeader>
          <CardTitle>Filters</CardTitle>
        </CardHeader>
        <CardContent className="">
          <div className="flex flex-wrap gap-4">
            <div className="mx-4 ml-0 flex flex-1 flex-col space-y-2">
              <Label htmlFor="search">Search</Label>
              <div className="relative">
                <Search className="absolute top-2.5 left-2 h-4 w-4 text-muted-foreground" />
                <Input
                  className="pl-8"
                  id="search"
                  onChange={(e) => setSearchTerm(e.target.value)}
                  placeholder="User, resource, action..."
                  value={searchTerm}
                />
              </div>
            </div>

            <div className="mx-4 flex flex-col space-y-2">
              <Label htmlFor="action-type">Action Type</Label>
              <NativeSelect
                className="min-w-[200px]"
                id="action-type"
                onChange={(event) => {
                  setActionTypeFilter(event.target.value)
                  setPage(1)
                }}
                value={actionTypeFilter}
              >
                {Object.entries(ACTION_TYPES).map(([value, label]) => (
                  <option key={value} value={value}>
                    {label}
                  </option>
                ))}
              </NativeSelect>
            </div>

            <div className="mx-4 flex flex-col space-y-2">
              <Label htmlFor="resource-type">Resource Type</Label>
              <NativeSelect
                className="min-w-[180px]"
                id="resource-type"
                onChange={(event) => {
                  setResourceTypeFilter(event.target.value)
                  setPage(1)
                }}
                value={resourceTypeFilter}
              >
                {Object.entries(RESOURCE_TYPES).map(([value, label]) => (
                  <option key={value} value={value}>
                    {label}
                  </option>
                ))}
              </NativeSelect>
            </div>

            <div className="mx-4 flex flex-col space-y-2">
              <Label htmlFor="status">Status</Label>
              <NativeSelect
                className="min-w-[180px]"
                id="status"
                onChange={(event) => {
                  setStatusFilter(event.target.value)
                  setPage(1)
                }}
                value={statusFilter}
              >
                {Object.entries(STATUS_TYPES).map(([value, label]) => (
                  <option key={value} value={value}>
                    {label}
                  </option>
                ))}
              </NativeSelect>
            </div>

            <div className="mx-4 flex flex-col space-y-2">
              <Label htmlFor="date-range">Date Range</Label>
              <NativeSelect
                className="min-w-[180px]"
                id="date-range"
                onChange={(event) => {
                  setDateRange(event.target.value)
                  setPage(1)
                }}
                value={dateRange}
              >
                <option value="15m">Last 15 Minutes</option>
                <option value="1h">Last Hour</option>
                <option value="24h">Last 24 Hours</option>
                <option value="7d">Last 7 Days</option>
                <option value="30d">Last 30 Days</option>
                <option value="all">All Time</option>
                <option value="custom">Custom…</option>
              </NativeSelect>
            </div>

            <div className="mx-6 mr-0 flex flex-col space-y-2">
              <Label htmlFor="per-page">Per Page</Label>
              <NativeSelect
                className="min-w-[80px]"
                id="per-page"
                onChange={(event) => {
                  setPerPage(Number(event.target.value))
                  setPage(1)
                }}
                value={String(perPage)}
              >
                <option value="10">10</option>
                <option value="25">25</option>
                <option value="50">50</option>
                <option value="100">100</option>
                <option value="200">200</option>
              </NativeSelect>
            </div>
          </div>

          {dateRange === 'custom' && (
            <div className="mt-8 flex w-full flex-col gap-12 sm:flex-row">
              <DateTimePickerField
                label="Start"
                maxValue={customEnd}
                onChange={(next) => {
                  handleCustomStartChange(next)
                }}
                value={customStart}
              />
              <DateTimePickerField
                label="End"
                minValue={customStart}
                onChange={(next) => {
                  handleCustomEndChange(next)
                }}
                value={customEnd}
              />
            </div>
          )}

          <div className="mt-8 flex gap-2">
            <Button onClick={() => refetch()} variant="outline">
              <RefreshCw className="mr-2 h-4 w-4" />
              Refresh
            </Button>
            <Button onClick={handleExport} variant="outline">
              <Download className="mr-2 h-4 w-4" />
              Export CSV
            </Button>
          </div>
        </CardContent>
      </Card>

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
        logs={pageItems}
        onNextPage={() => setPage((p) => Math.min(totalPages, p + 1))}
        onPreviousPage={() => setPage((p) => Math.max(1, p - 1))}
        title={title}
        totalCount={totalCount}
        totalPages={totalPages}
      />
    </div>
  )
}

type DateTimePickerFieldProps = {
  label: string
  value: string
  onChange: (next: string) => void
  minValue?: string
  maxValue?: string
}

function DateTimePickerField({
  label,
  value,
  onChange,
  minValue,
  maxValue,
}: DateTimePickerFieldProps) {
  const { datePart, timePart } = useMemo(() => splitDateTime(value), [value])
  const [open, setOpen] = useState(false)
  const selectedDate = useMemo(() => parseDateTime(value), [value])
  const [month, setMonth] = useState<Date>(selectedDate ?? new Date())

  useEffect(() => {
    if (!selectedDate) {
      return
    }
    setMonth((current) => {
      if (current && selectedDate.getTime() === current.getTime()) {
        return current
      }
      return selectedDate
    })
  }, [selectedDate])
  const minParts = useMemo(() => splitDateTime(minValue), [minValue])
  const maxParts = useMemo(() => splitDateTime(maxValue), [maxValue])
  const timeMin = minValue && minParts.datePart === datePart ? minParts.timePart : undefined
  const timeMax = maxValue && maxParts.datePart === datePart ? maxParts.timePart : undefined

  const safeLabel = label.toLowerCase().replace(/\s+/g, '-')
  const dateInputId = `${safeLabel}-date`
  const timeInputId = `${safeLabel}-time`

  return (
    <div className="flex w-full flex-col space-y-2 sm:w-auto sm:min-w-[220px]">
      <Label htmlFor={dateInputId}>{label}</Label>
      <div className="relative">
        <Input
          className="pr-10 font-mono text-sm sm:w-[250px]"
          id={dateInputId}
          onChange={(event) => {
            const nextDate = event.target.value.trim()
            const combined = combineDateTime(nextDate, timePart)
            onChange(combined)
            const parsed = parseDateTime(combined)
            if (parsed) {
              setMonth(parsed)
            }
          }}
          onKeyDown={(event) => {
            if (event.key === 'ArrowDown') {
              event.preventDefault()
              setOpen(true)
            }
          }}
          placeholder="YYYY-MM-DD"
          value={datePart}
        />
        <Popover onOpenChange={setOpen} open={open}>
          <PopoverTrigger asChild>
            <Button
              className="-translate-y-1/2 absolute top-1/2 right-2 h-8 w-8 p-0 text-muted-foreground hover:bg-transparent focus-visible:ring-1 dark:text-muted-foreground"
              variant="ghost"
            >
              <CalendarIcon className="h-4 w-4" />
              <span className="sr-only">Open calendar for {label}</span>
            </Button>
          </PopoverTrigger>
          <PopoverContent
            align="end"
            alignOffset={-4}
            className="w-auto overflow-hidden p-0"
            sideOffset={8}
          >
            <Calendar
              initialFocus
              mode="single"
              month={month}
              onMonthChange={setMonth}
              onSelect={(selectedDateValue: Date | undefined) => {
                if (!selectedDateValue) {
                  return
                }
                const isoDate = format(selectedDateValue, DATE_ONLY_FORMAT)
                const combined = combineDateTime(isoDate, timePart)
                onChange(combined)
                setOpen(false)
              }}
              selected={selectedDate ?? undefined}
            />
          </PopoverContent>
        </Popover>
      </div>
      <div className="flex items-center gap-2 sm:max-w-[250px]">
        <Label className="sr-only" htmlFor={timeInputId}>
          {label} time
        </Label>
        <Input
          className="bg-background font-mono text-sm sm:w-[120px] dark:bg-background [&::-webkit-calendar-picker-indicator]:hidden dark:[&::-webkit-calendar-picker-indicator]:invert"
          id={timeInputId}
          max={timeMax || undefined}
          min={timeMin || undefined}
          onChange={(event) => {
            const nextTime = event.target.value
            const combined = combineDateTime(datePart, nextTime)
            onChange(combined)
          }}
          step="60"
          type="time"
          value={timePart}
        />
        <span className="text-muted-foreground text-xs">HH:MM</span>
      </div>
    </div>
  )
}
