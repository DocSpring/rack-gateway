import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { format } from 'date-fns'
import { Download, Eye, RefreshCw, Search } from 'lucide-react'
import { useEffect, useState } from 'react'
import { TablePane } from '../components/table-pane'
import { TimeAgo } from '../components/time-ago'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/card'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '../components/ui/dialog'
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

interface AuditLog {
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

// Extract trailing "(id: N)" from legacy resource strings
const RESOURCE_ID_TAIL_RE = /\s*\(id:\s*\d+\)\s*$/
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
    label = (log.resource || '').replace(RESOURCE_ID_TAIL_RE, '').trim() || '-'
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

// Regex helpers for legacy resource strings like "Name (id: 12)"

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

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: acceptable complexity for this page component
export function AuditPage() {
  const [selected, setSelected] = useState<AuditLog | null>(null)
  const [searchTerm, setSearchTerm] = useState('')
  const [debouncedSearch, setDebouncedSearch] = useState('')
  const [actionTypeFilter, setActionTypeFilter] = useState('all')
  const [statusFilter, setStatusFilter] = useState('all')
  const [resourceTypeFilter, setResourceTypeFilter] = useState('all')
  const [dateRange, setDateRange] = useState(() => {
    try {
      return localStorage.getItem('audit_date_range') || '7d'
    } catch {
      return '7d'
    }
  })
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = useState<number>(() => {
    try {
      const v = localStorage.getItem('audit_per_page')
      return v ? Math.max(1, Number.parseInt(v, 10)) : DEFAULT_PER_PAGE
    } catch {
      return DEFAULT_PER_PAGE
    }
  })

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

  // Debounce search to prevent refetch on every keystroke
  useEffect(() => {
    const t = setTimeout(() => setDebouncedSearch(searchTerm), 300)
    return () => clearTimeout(t)
  }, [searchTerm])

  // Fetch audit logs
  const {
    data: logs = [],
    error,
    isError,
    isLoading,
    refetch,
  } = useQuery<AuditLog[], Error>({
    queryKey: [
      'audit-logs',
      actionTypeFilter,
      statusFilter,
      resourceTypeFilter,
      dateRange,
      debouncedSearch,
    ],
    queryFn: async () => {
      const params = new URLSearchParams()
      if (actionTypeFilter !== 'all') {
        params.append('action_type', actionTypeFilter)
      }
      if (statusFilter !== 'all') {
        params.append('status', statusFilter)
      }
      if (resourceTypeFilter !== 'all') {
        params.append('resource_type', resourceTypeFilter)
      }
      if (dateRange) {
        params.append('range', dateRange)
      }
      if (debouncedSearch) {
        params.append('search', debouncedSearch)
      }

      const response = await api.get<AuditLog[]>(`/.gateway/api/admin/audit?${params}`)
      return response
    },
    placeholderData: keepPreviousData,
    refetchOnMount: 'always',
    refetchOnWindowFocus: true,
    staleTime: 0,
  })

  const handleExport = () => {
    const params = new URLSearchParams()
    if (actionTypeFilter !== 'all') {
      params.append('action_type', actionTypeFilter)
    }
    if (statusFilter !== 'all') {
      params.append('status', statusFilter)
    }
    if (resourceTypeFilter !== 'all') {
      params.append('resource_type', resourceTypeFilter)
    }
    if (dateRange) {
      params.append('range', dateRange)
    }
    if (searchTerm) {
      params.append('search', searchTerm)
    }
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
  const stats = {
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

  // Client-side pagination
  const totalPages = Math.max(1, Math.ceil(stats.total / perPage))
  const currentPage = Math.min(page, totalPages)
  const startIdx = (currentPage - 1) * perPage
  const endIdx = Math.min(startIdx + perPage, stats.total)
  const pageItems = logs.slice(startIdx, endIdx)

  return (
    <div className="p-8">
      <div className="mb-8">
        <h1 className="font-bold text-3xl">Audit Logs</h1>
        <p className="mt-2 text-muted-foreground">
          Monitor all gateway activity and access patterns
        </p>
      </div>

      {/* Statistics Cards */}
      <div className="mb-6 grid gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="font-medium text-muted-foreground text-sm">Total Logs</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="font-bold text-2xl">{stats.total}</div>
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
              {stats.total > 0 ? Math.round((stats.success / stats.total) * 100) : 0}%
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
            <div className="font-bold text-2xl text-red-600">{stats.failed + stats.denied}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="font-medium text-muted-foreground text-sm">
              Avg Response Time
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="font-bold text-2xl">{stats.avgResponseTime}ms</div>
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
              <Select onValueChange={setActionTypeFilter} value={actionTypeFilter}>
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
              <Select onValueChange={setResourceTypeFilter} value={resourceTypeFilter}>
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
              <Select onValueChange={setStatusFilter} value={statusFilter}>
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
              <Select onValueChange={setDateRange} value={dateRange}>
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

          <div className="mt-4 flex gap-2">
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
        description={`Showing ${stats.total} logs · Page ${currentPage} of ${totalPages}`}
        empty={stats.total === 0 && !isError}
        emptyMessage="No audit logs found"
        error={
          isError
            ? `Failed to load audit logs: ${String((error as Error)?.message || 'Unknown error')}`
            : null
        }
        loading={!!(isLoading && logs.length === 0)}
        title="Audit Logs"
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

        {stats.total > 0 && (
          <div className="mt-4 flex items-center justify-between">
            <div className="text-muted-foreground text-sm">
              Showing {startIdx + 1}–{endIdx} of {stats.total} logs
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
