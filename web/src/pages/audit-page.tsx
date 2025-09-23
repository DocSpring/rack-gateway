import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { format } from 'date-fns'
import { Calendar as CalendarIcon, Download, Eye, RefreshCw, Search } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Calendar } from '@/components/ui/calendar'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { TablePane } from '../components/table-pane'
import { TimeAgo } from '../components/time-ago'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '../components/ui/dialog'
import { Input } from '../components/ui/input'
import { Label } from '../components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '../components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '../components/ui/table'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../components/ui/tooltip'
import { api } from '../lib/api'
import { DEFAULT_PER_PAGE } from '../lib/constants'

type AuditLog = {
  id: number
  timestamp: string
  user_email: string
  user_name: string
  action_type: string
  action: string
  command?: string
  resource: string
  resource_type?: string
  details: string
  ip_address: string
  user_agent: string
  status: string
  rbac_decision?: string
  http_status?: number
  response_time_ms: number
}

const MAX_LABEL_LEN = 23

function safeParseDetails(details: string): Record<string, unknown> {
  try {
    return JSON.parse(details || '{}') as Record<string, unknown>
  } catch {
    return {}
  }
}

function resourceLabelForLog(log: AuditLog): string {
  const d = safeParseDetails(log.details)
  let label = ''
  if (log.action_type === 'users' || log.action.startsWith('user.')) {
    label = (d.email as string) || ''
  } else if (log.action_type === 'tokens' || log.action.startsWith('api_token.')) {
    label = (d.name as string) || ''
  }
  if (!label) {
    label = (log.resource || '').trim() || '-'
  }
  return label
}

function LabelBadge({ label }: { label: string }) {
  const needsTruncate = label.length > MAX_LABEL_LEN
  const shortText = needsTruncate ? `${label.slice(0, MAX_LABEL_LEN - 3)}...` : label
  const content = (
    <Badge
      className="border border-border bg-muted font-mono text-muted-foreground"
      variant="outline"
    >
      {shortText || '-'}
    </Badge>
  )
  if (!needsTruncate) {
    return content
  }
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>{content}</TooltipTrigger>
        <TooltipContent>
          <span className="font-mono">{label}</span>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}

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

  const [selected, setSelected] = useState<AuditLog | null>(null)
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
    { logs: AuditLog[]; total: number; page: number; limit: number },
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
        logs: AuditLog[]
        total: number
        page: number
        limit: number
      }>(`/.gateway/api/admin/audit?${params}`)
    },
    placeholderData: keepPreviousData,
    refetchOnMount: 'always',
    refetchOnWindowFocus: true,
    staleTime: 0,
  })

  const logs = data?.logs ?? []
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
    const url = `/.gateway/api/admin/audit/export?${params}`
    const link = document.createElement('a')
    link.href = url
    link.download = `audit-logs-${format(new Date(), 'yyyy-MM-dd')}.csv`
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
  }

  const getStatusBadgeAppearance = (
    status: string
  ): {
    variant: 'default' | 'secondary' | 'destructive' | 'outline'
    className?: string
  } => {
    switch (status) {
      case 'success':
        // Force green for success regardless of theme primary color
        return {
          variant: 'default',
          className: 'bg-green-600 text-white hover:bg-green-700',
        }
      case 'failed':
      case 'error':
      case 'blocked':
      case 'denied':
        // Red for failure/error/blocked
        return { variant: 'destructive' }
      default:
        return { variant: 'outline' }
    }
  }

  const getActionTypeBadgeAppearance = (
    type: string
  ): {
    variant: 'default' | 'secondary' | 'destructive' | 'outline'
    className?: string
  } => {
    // Color only the Type badge for quick scanning; keep subtle but distinct
    switch (type) {
      case 'auth':
        return {
          variant: 'outline',
          className: 'bg-blue-600 text-white border border-border',
        }
      case 'users':
        return {
          variant: 'default',
          className: 'bg-blue-600 text-white',
        }
      case 'tokens':
        return {
          variant: 'default',
          className: 'bg-purple-600 text-white',
        }
      case 'convox':
        return {
          variant: 'default',
          className: 'bg-slate-700 text-white',
        }
      default:
        return {
          variant: 'outline',
          className: 'bg-muted text-muted-foreground border border-border',
        }
    }
  }

  const getResourceTypeBadgeAppearance = (
    type?: string
  ): {
    variant: 'default' | 'secondary' | 'destructive' | 'outline'
    className?: string
  } => {
    switch (type) {
      case 'app':
        return {
          variant: 'default',
          className: 'bg-slate-500 text-white',
        }
      case 'rack':
        return {
          variant: 'default',
          className: 'bg-slate-700 text-white',
        }
      case 'env':
        return {
          variant: 'default',
          className: 'bg-amber-700 text-white',
        }
      case 'process':
        return {
          variant: 'default',
          className: 'bg-yellow-200 text-black',
        }
      case 'secret':
        return {
          variant: 'default',
          className: 'bg-amber-400 text-black',
        }
      case 'system':
        return {
          variant: 'default',
          className: 'bg-slate-700 text-white',
        }
      case 'api_token':
        return {
          variant: 'default',
          className: 'bg-purple-600 text-white',
        }
      case 'user':
      case 'auth':
        return {
          variant: 'default',
          className: 'bg-blue-600 text-white',
        }
      default:
        return {
          variant: 'outline',
          className: 'bg-muted text-muted-foreground border border-border',
        }
    }
  }

  // Do not unmount the page on loading/error; render inline status instead to preserve input focus

  // Calculate statistics
  const pageStats = {
    total: logs.length,
    success: logs.filter((l: AuditLog) => l.status === 'success').length,
    failed: logs.filter((l: AuditLog) => l.status === 'failed').length,
    denied: logs.filter((l: AuditLog) => l.status === 'denied' || l.status === 'blocked').length,
    avgResponseTime:
      logs.length > 0
        ? Math.round(
            logs.reduce((acc: number, l: AuditLog) => acc + l.response_time_ms, 0) / logs.length
          )
        : 0,
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
              <Select
                onValueChange={(value) => {
                  setActionTypeFilter(value)
                  setPage(1)
                }}
                value={actionTypeFilter}
              >
                <SelectTrigger className="min-w-[200px]" id="action-type">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {Object.entries(ACTION_TYPES).map(([value, label]) => (
                    <SelectItem key={value} value={value}>
                      {label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="mx-4 flex flex-col space-y-2">
              <Label htmlFor="resource-type">Resource Type</Label>
              <Select
                onValueChange={(value) => {
                  setResourceTypeFilter(value)
                  setPage(1)
                }}
                value={resourceTypeFilter}
              >
                <SelectTrigger className="min-w-[180px]" id="resource-type">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {Object.entries(RESOURCE_TYPES).map(([value, label]) => (
                    <SelectItem key={value} value={value}>
                      {label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="mx-4 flex flex-col space-y-2">
              <Label htmlFor="status">Status</Label>
              <Select
                onValueChange={(value) => {
                  setStatusFilter(value)
                  setPage(1)
                }}
                value={statusFilter}
              >
                <SelectTrigger className="min-w-[180px]" id="status">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {Object.entries(STATUS_TYPES).map(([value, label]) => (
                    <SelectItem key={value} value={value}>
                      {label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="mx-4 flex flex-col space-y-2">
              <Label htmlFor="date-range">Date Range</Label>
              <Select
                onValueChange={(value) => {
                  setDateRange(value)
                  setPage(1)
                }}
                value={dateRange}
              >
                <SelectTrigger className="min-w-[180px]" id="date-range">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="15m">Last 15 Minutes</SelectItem>
                  <SelectItem value="1h">Last Hour</SelectItem>
                  <SelectItem value="24h">Last 24 Hours</SelectItem>
                  <SelectItem value="7d">Last 7 Days</SelectItem>
                  <SelectItem value="30d">Last 30 Days</SelectItem>
                  <SelectItem value="all">All Time</SelectItem>
                  <SelectItem value="custom">Custom…</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="mx-6 mr-0 flex flex-col space-y-2">
              <Label htmlFor="per-page">Per Page</Label>
              <Select
                onValueChange={(v) => {
                  setPerPage(Number(v))
                  setPage(1)
                }}
                value={String(perPage)}
              >
                <SelectTrigger className="min-w-[80px]" id="per-page">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="25">25</SelectItem>
                  <SelectItem value="50">50</SelectItem>
                  <SelectItem value="100">100</SelectItem>
                  <SelectItem value="200">200</SelectItem>
                </SelectContent>
              </Select>
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

      {/* Logs Table */}
      <TablePane
        description={`Showing ${logs.length} of ${totalCount} logs · Page ${currentPage} of ${totalPages}`}
        empty={logs.length === 0 && !isError}
        emptyMessage="No audit logs found"
        error={
          isError
            ? `Failed to load audit logs: ${String((error as Error)?.message || 'Unknown error')}`
            : null
        }
        loading={!!(isLoading && logs.length === 0)}
        title={title}
      >
        <Table className="text-sm">
          <TableHeader>
            <TableRow>
              <TableHead>User</TableHead>
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
            {pageItems.map((log: AuditLog) => (
              <TableRow
                className="cursor-pointer hover:bg-accent/50"
                key={log.id}
                onClick={() => setSelected(log)}
              >
                <TableCell>
                  <div>
                    <div className="font-medium">{log.user_email}</div>
                    {log.user_name && (
                      <div className="text-muted-foreground text-xs">{log.user_name}</div>
                    )}
                  </div>
                </TableCell>
                <TableCell>
                  {(() => {
                    const ap = getActionTypeBadgeAppearance(log.action_type)
                    return (
                      <Badge className={ap.className} variant={ap.variant}>
                        {log.action_type.replace('_', ' ')}
                      </Badge>
                    )
                  })()}
                </TableCell>
                <TableCell className="text-sm">
                  {/* biome-ignore lint/complexity/noExcessiveCognitiveComplexity: complex action rendering for exec */}
                  {(() => {
                    if (log.action_type === 'convox' && log.action === 'process.exec') {
                      const raw = (() => {
                        try {
                          const d = JSON.parse(log.details || '{}') as {
                            command?: string
                          }
                          return (log.command || d.command || '').trim()
                        } catch {
                          return (log.command || '').trim()
                        }
                      })()
                      let cmd = raw
                      if (
                        (cmd.startsWith("'") && cmd.endsWith("'")) ||
                        (cmd.startsWith('"') && cmd.endsWith('"'))
                      ) {
                        cmd = cmd.slice(1, -1)
                      }
                      const truncated = cmd.length > 64 ? `${cmd.slice(0, 64)}…` : cmd
                      return (
                        <div className="flex flex-col">
                          <Badge
                            className="w-fit border border-border bg-muted font-mono text-muted-foreground"
                            variant="outline"
                          >
                            {log.action}
                          </Badge>
                          {cmd && (
                            <code
                              className="mt-1 w-fit whitespace-nowrap rounded border border-border bg-secondary px-1 py-0.5 font-mono text-blue-600 shadow-sm dark:text-blue-300"
                              title={cmd}
                            >
                              {truncated}
                            </code>
                          )}
                        </div>
                      )
                    }
                    return (
                      <Badge
                        className="border border-border bg-muted font-mono text-muted-foreground"
                        variant="outline"
                      >
                        {log.action}
                      </Badge>
                    )
                  })()}
                </TableCell>
                <TableCell>
                  {(() => {
                    const rt = log.resource_type || log.action_type?.split('.')[0] || 'unknown'
                    const ap = getResourceTypeBadgeAppearance(rt)
                    return (
                      <Badge className={ap.className} variant={ap.variant}>
                        {rt}
                      </Badge>
                    )
                  })()}
                </TableCell>
                <TableCell>
                  <LabelBadge label={resourceLabelForLog(log)} />
                </TableCell>
                <TableCell>
                  {(() => {
                    const ap = getStatusBadgeAppearance(log.status)
                    const statusLabel = (() => {
                      if (log.status === 'denied') {
                        return 'denied (RBAC)'
                      }
                      if ((log.status === 'failed' || log.status === 'error') && log.http_status) {
                        return `${log.status} (${log.http_status})`
                      }
                      return log.status
                    })()
                    return (
                      <Badge className={ap.className} variant={ap.variant}>
                        {statusLabel}
                      </Badge>
                    )
                  })()}
                </TableCell>
                <TableCell className="font-mono text-sm">{log.ip_address || '-'}</TableCell>
                <TableCell className="font-mono text-sm">
                  <TimeAgo date={log.timestamp} />
                </TableCell>
                <TableCell className="text-right" onClick={(e) => e.stopPropagation()}>
                  <Button onClick={() => setSelected(log)} size="sm" variant="ghost">
                    <Eye className="h-4 w-4" />
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>

        {totalCount > 0 && (
          <div className="mt-4 flex items-center justify-between">
            <div className="text-muted-foreground text-sm">
              Showing {firstRowIndex === 0 ? 0 : firstRowIndex}–{lastRowIndex} of {totalCount} logs
            </div>
            <div className="flex gap-2">
              <Button
                disabled={currentPage === 1}
                onClick={() => setPage((p) => Math.max(1, p - 1))}
                variant="outline"
              >
                Previous
              </Button>
              <Button
                disabled={currentPage === totalPages}
                onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                variant="outline"
              >
                Next
              </Button>
            </div>
          </div>
        )}
      </TablePane>

      {/* Centered detail modal */}
      <Dialog onOpenChange={(open) => !open && setSelected(null)} open={!!selected}>
        <DialogContent className="max-h-[80vh] max-w-2xl overflow-auto">
          <DialogHeader>
            <DialogTitle>Audit Log</DialogTitle>
            <DialogDescription>
              Detailed information for the selected audit log entry:
            </DialogDescription>
          </DialogHeader>
          {selected && (
            <div className="space-y-3 text-sm">
              <div>
                <span className="text-muted-foreground">Timestamp:</span>{' '}
                {new Date(selected.timestamp).toISOString()}
              </div>
              <div>
                <span className="text-muted-foreground">User:</span> {selected.user_email}{' '}
                {selected.user_name ? `(${selected.user_name})` : ''}
              </div>
              <div>
                <span className="text-muted-foreground">Type:</span> {selected.action_type}
              </div>
              <div>
                <span className="text-muted-foreground">Action:</span> {selected.action}
              </div>
              <div>
                <span className="text-muted-foreground">Resource:</span> {selected.resource || '-'}
              </div>
              <div>
                <span className="text-muted-foreground">Resource Type:</span>{' '}
                {selected.resource_type || selected.action_type?.split('.')[0] || 'unknown'}
              </div>
              <div>
                <span className="text-muted-foreground">Status:</span> {(() => {
                  if (selected.status === 'denied') {
                    return 'denied (RBAC)'
                  }
                  if (
                    (selected.status === 'failed' || selected.status === 'error') &&
                    selected.http_status
                  ) {
                    return `${selected.status} (${selected.http_status})`
                  }
                  return selected.status
                })()}
              </div>
              {selected.rbac_decision && (
                <div>
                  <span className="text-muted-foreground">RBAC:</span> {selected.rbac_decision}
                </div>
              )}
              {typeof selected.http_status === 'number' && selected.http_status > 0 && (
                <div>
                  <span className="text-muted-foreground">HTTP Status:</span> {selected.http_status}
                </div>
              )}
              <div>
                <span className="text-muted-foreground">Response Time:</span>{' '}
                {selected.response_time_ms} ms
              </div>
              <div>
                <span className="text-muted-foreground">IP:</span> {selected.ip_address || '-'}
              </div>
              <div className="break-all">
                <span className="text-muted-foreground">User Agent:</span>{' '}
                {selected.user_agent || '-'}
              </div>
              {selected.command && (
                <div className="break-all">
                  <span className="text-muted-foreground">Command:</span>{' '}
                  <code className="rounded border bg-secondary px-1 py-0.5">
                    {selected.command}
                  </code>
                </div>
              )}
              <div className="break-all">
                <span className="text-muted-foreground">Details:</span>
                <pre className="mt-2 max-h-64 overflow-auto rounded bg-muted p-2 text-xs">
                  {(() => {
                    try {
                      return JSON.stringify(JSON.parse(selected.details || '{}'), null, 2)
                    } catch {
                      return selected.details || '-'
                    }
                  })()}
                </pre>
              </div>
              <div className="mt-2 flex justify-end">
                <Button onClick={() => setSelected(null)} variant="outline">
                  Close
                </Button>
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>
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
