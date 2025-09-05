import { useQuery } from '@tanstack/react-query'
import { format } from 'date-fns'
import { Download, RefreshCw, Search } from 'lucide-react'
import { useState } from 'react'
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
  const [actionTypeFilter, setActionTypeFilter] = useState('all')
  const [statusFilter, setStatusFilter] = useState('all')
  const [dateRange, setDateRange] = useState('7d')

  // Fetch audit logs
  const {
    data: logs = [],
    isLoading,
    error,
    refetch,
  } = useQuery({
    queryKey: ['audit-logs', actionTypeFilter, statusFilter, dateRange, searchTerm],
    queryFn: async () => {
      const params = new URLSearchParams()
      if (actionTypeFilter !== 'all') { params.append('action_type', actionTypeFilter) }
      if (statusFilter !== 'all') { params.append('status', statusFilter) }
      if (dateRange) { params.append('range', dateRange) }
      if (searchTerm) { params.append('search', searchTerm) }

      const response = await api.get<AuditLog[]>(`/.gateway/admin/audit?${params}`)
      return response
    },
  })

  const handleExport = () => {
    const params = new URLSearchParams()
    if (actionTypeFilter !== 'all') { params.append('action_type', actionTypeFilter) }
    if (statusFilter !== 'all') { params.append('status', statusFilter) }
    if (dateRange) { params.append('range', dateRange) }
    if (searchTerm) { params.append('search', searchTerm) }
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

  const getStatusBadgeVariant = (status: string) => {
    switch (status) {
      case 'success':
        return 'default'
      case 'failed':
        return 'destructive'
      case 'blocked':
        return 'secondary'
      default:
        return 'outline'
    }
  }

  const getActionTypeBadgeVariant = (type: string) => {
    switch (type) {
      case 'auth':
        return 'default'
      case 'user_management':
        return 'secondary'
      case 'token_management':
        return 'outline'
      default:
        return 'default'
    }
  }

  if (isLoading) {
    return (
      <div className="p-8">
        <div className="flex h-64 items-center justify-center">
          <RefreshCw className="h-8 w-8 animate-spin text-muted-foreground" />
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="p-8">
        <Card>
          <CardHeader>
            <CardTitle className="text-destructive">Error</CardTitle>
            <CardDescription>Failed to load audit logs</CardDescription>
          </CardHeader>
        </Card>
      </div>
    )
  }

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
          <div className="grid gap-4 md:grid-cols-4">
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
                  <SelectItem value="1h">Last Hour</SelectItem>
                  <SelectItem value="24h">Last 24 Hours</SelectItem>
                  <SelectItem value="7d">Last 7 Days</SelectItem>
                  <SelectItem value="30d">Last 30 Days</SelectItem>
                  <SelectItem value="all">All Time</SelectItem>
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
          <CardDescription>Showing {logs.length} events</CardDescription>
        </CardHeader>
        <CardContent>
          {logs.length === 0 ? (
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
                  <TableHead>Response Time</TableHead>
                  <TableHead>IP Address</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {logs.map((log: AuditLog) => (
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
                      <Badge variant={getActionTypeBadgeVariant(log.action_type)}>
                        {log.action_type.replace('_', ' ')}
                      </Badge>
                    </TableCell>
                    <TableCell className="font-mono text-sm">{log.action}</TableCell>
                    <TableCell>
                      <div className="max-w-[200px] truncate" title={log.resource}>
                        {log.resource || '-'}
                      </div>
                    </TableCell>
                    <TableCell>
                      <Badge variant={getStatusBadgeVariant(log.status)}>{log.status}</Badge>
                    </TableCell>
                    <TableCell className="text-right">{log.response_time_ms}ms</TableCell>
                    <TableCell className="font-mono text-sm">{log.ip_address || '-'}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
