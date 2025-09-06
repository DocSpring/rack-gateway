import { useQuery } from '@tanstack/react-query'
import { format } from 'date-fns'
import { Download, RefreshCw, Search } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card'
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
import { api } from '../lib/api'

interface AuditLog {
  id: number
  timestamp: string
  user_email: string
  user_name: string
  action_type: string
  action: string
  command?: string
  resource: string
  details: string
  ip_address: string
  user_agent: string
  status: string
  response_time_ms: number
}

const ACTION_TYPES = {
  all: 'All Actions',
  convox_api: 'Convox API',
  auth: 'Authentication',
  user_management: 'User Management',
  token_management: 'Token Management',
}

const STATUS_TYPES = {
  all: 'All Statuses',
  success: 'Success',
  failed: 'Failed',
  blocked: 'Blocked',
}

export function AuditPage() {
  const [searchTerm, setSearchTerm] = useState('')
  const [debouncedSearch, setDebouncedSearch] = useState('')
  const [actionTypeFilter, setActionTypeFilter] = useState('all')
  const [statusFilter, setStatusFilter] = useState('all')
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
      return v ? Math.max(1, parseInt(v, 10)) : 50
    } catch {
      return 50
    }
  })

  // Persist selected date range and per-page to localStorage
  useEffect(() => {
    try {
      localStorage.setItem('audit_date_range', dateRange)
    } catch {}
  }, [dateRange])
  useEffect(() => {
    try {
      localStorage.setItem('audit_per_page', String(perPage))
    } catch {}
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
    isFetching,
    refetch,
  } = useQuery({
    queryKey: ['audit-logs', actionTypeFilter, statusFilter, dateRange, debouncedSearch],
    queryFn: async () => {
      const params = new URLSearchParams()
      if (actionTypeFilter !== 'all') {
        params.append('action_type', actionTypeFilter)
      }
      if (statusFilter !== 'all') {
        params.append('status', statusFilter)
      }
      if (dateRange) {
        params.append('range', dateRange)
      }
      if (debouncedSearch) {
        params.append('search', debouncedSearch)
      }

      const response = await api.get<AuditLog[]>(`/.gateway/admin/audit?${params}`)
      return response
    },
    keepPreviousData: true,
  })

  const handleExport = () => {
    const params = new URLSearchParams()
    if (actionTypeFilter !== 'all') {
      params.append('action_type', actionTypeFilter)
    }
    if (statusFilter !== 'all') {
      params.append('status', statusFilter)
    }
    if (dateRange) {
      params.append('range', dateRange)
    }
    if (searchTerm) {
      params.append('search', searchTerm)
    }
    params.append('format', 'csv')

    // Create download link
    const url = `/.gateway/admin/audit/export?${params}`
    const link = document.createElement('a')
    link.href = url
    link.download = `audit-logs-${format(new Date(), 'yyyy-MM-dd')}.csv`
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
  }

  const getStatusBadgeAppearance = (
    status: string
  ): { variant: 'default' | 'secondary' | 'destructive' | 'outline'; className?: string } => {
    switch (status) {
      case 'success':
        // Force green for success regardless of theme primary color
        return { variant: 'default', className: 'bg-green-600 text-white hover:bg-green-700' }
      case 'failed':
      case 'error':
        // Red for failure/error
        return { variant: 'destructive' }
      case 'blocked':
        return { variant: 'secondary' }
      default:
        return { variant: 'outline' }
    }
  }

  const getActionTypeBadgeAppearance = (
    type: string
  ): { variant: 'default' | 'secondary' | 'destructive' | 'outline'; className?: string } => {
    switch (type) {
      case 'auth':
        return { variant: 'default' }
      case 'user_management':
        // Purple badge for user management actions
        return { variant: 'default', className: 'bg-purple-600 text-white hover:bg-purple-700' }
      case 'token_management':
        return { variant: 'outline' }
      default:
        return { variant: 'default' }
    }
  }

  // Do not unmount the page on loading/error; render inline status instead to preserve input focus

  // Calculate statistics
  const stats = {
    total: logs.length,
    success: logs.filter((l: AuditLog) => l.status === 'success').length,
    failed: logs.filter((l: AuditLog) => l.status === 'failed').length,
    blocked: logs.filter((l: AuditLog) => l.status === 'blocked').length,
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
            <CardTitle className="font-medium text-muted-foreground text-sm">
              Total Events
            </CardTitle>
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
              Failed/Blocked
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="font-bold text-2xl text-red-600">{stats.failed + stats.blocked}</div>
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
        <CardContent>
          <div className="grid gap-4 md:grid-cols-5">
            <div className="space-y-2">
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

            <div className="space-y-2">
              <Label htmlFor="action-type">Action Type</Label>
              <Select onValueChange={setActionTypeFilter} value={actionTypeFilter}>
                <SelectTrigger id="action-type">
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

            <div className="space-y-2">
              <Label htmlFor="status">Status</Label>
              <Select onValueChange={setStatusFilter} value={statusFilter}>
                <SelectTrigger id="status">
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

            <div className="space-y-2">
              <Label htmlFor="date-range">Date Range</Label>
              <Select onValueChange={setDateRange} value={dateRange}>
                <SelectTrigger id="date-range">
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

            <div className="space-y-2">
              <Label htmlFor="per-page">Per Page</Label>
              <Select
                onValueChange={(v) => {
                  setPerPage(Number(v))
                  setPage(1)
                }}
                value={String(perPage)}
              >
                <SelectTrigger id="per-page">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
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
      <Card>
        <CardHeader>
          <CardTitle>Activity Log</CardTitle>
          <CardDescription>
            Showing {stats.total} events · Page {currentPage} of {totalPages}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {isError && (
            <div className="mb-4 rounded-md border border-destructive/50 bg-destructive/10 p-3 text-sm text-destructive">
              Failed to load audit logs: {String((error as Error)?.message || 'Unknown error')}
            </div>
          )}
          {stats.total === 0 && !isError ? (
            <div className="py-8 text-center text-muted-foreground">No audit logs found</div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Timestamp</TableHead>
                  <TableHead>User</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Action</TableHead>
                  <TableHead>Resource</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>IP Address</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {pageItems.map((log: AuditLog) => (
                  <TableRow key={log.id}>
                    <TableCell className="font-mono text-sm">
                      {format(new Date(log.timestamp), 'MMM d, HH:mm:ss')}
                    </TableCell>
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
                    <TableCell className="font-mono text-sm">{log.action}</TableCell>
                    <TableCell>
                      <div className="max-w-[260px] truncate" title={log.resource}>
                        {(() => {
                          if (log.action_type === 'convox_api' && log.action === 'process.exec') {
                            const cmdFromColumn = log.command || ''
                            try {
                              const d = JSON.parse(log.details || '{}') as { command?: string }
                              const cmd = cmdFromColumn || d.command || ''
                              return (
                                <span>
                                  {log.resource}
                                  {cmd ? (
                                    <>
                                      {': '}
                                      <code className="whitespace-nowrap">{cmd}</code>
                                    </>
                                  ) : null}
                                </span>
                              )
                            } catch {
                              // Fallback
                              return (
                                <span>
                                  {log.resource}
                                  {cmdFromColumn ? (
                                    <>
                                      {': '}
                                      <code className="whitespace-nowrap">{cmdFromColumn}</code>
                                    </>
                                  ) : null}
                                </span>
                              )
                            }
                          }
                          return <span>{log.resource || '-'}</span>
                        })()}
                      </div>
                    </TableCell>
                    <TableCell>
                      {(() => {
                        const ap = getStatusBadgeAppearance(log.status)
                        return (
                          <Badge className={ap.className} variant={ap.variant}>
                            {log.status}
                          </Badge>
                        )
                      })()}
                    </TableCell>
                    <TableCell className="font-mono text-sm">{log.ip_address || '-'}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}

          {stats.total > 0 && (
            <div className="mt-4 flex items-center justify-between">
              <div className="text-muted-foreground text-sm">
                Showing {startIdx + 1}–{endIdx} of {stats.total}
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
        </CardContent>
      </Card>
    </div>
  )
}
