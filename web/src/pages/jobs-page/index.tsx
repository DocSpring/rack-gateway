import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import type { components } from '../../api/types.generated'
import { TablePane } from '../../components/table-pane'
import { Badge } from '../../components/ui/badge'
import { Button } from '../../components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '../../components/ui/table'
import { api } from '../../lib/api'
import { DEFAULT_PER_PAGE } from '../../lib/constants'

type JobListResponse = components['schemas']['handlers.JobListResponse']
type JobResponse = components['schemas']['handlers.JobResponse']

export function JobsPage() {
  return <JobsPageInner />
}

function JobsPageInner() {
  const [page, setPage] = useState(1)
  const [stateFilter, setStateFilter] = useState<string>('')
  const [queueFilter, setQueueFilter] = useState<string>('')

  const { data, isLoading, error } = useQuery({
    queryKey: ['jobs', stateFilter, queueFilter, page],
    queryFn: async () => {
      const params = new URLSearchParams()
      if (stateFilter) {
        params.append('state', stateFilter)
      }
      if (queueFilter) {
        params.append('queue', queueFilter)
      }
      const queryString = params.toString()
      const url = `/api/v1/jobs${queryString ? `?${queryString}` : ''}`
      const response = await api.get<JobListResponse>(url)
      return response
    },
  })

  const jobs = data?.jobs || []
  const count = data?.count || 0
  const limit = data?.limit || DEFAULT_PER_PAGE

  const perPage = limit
  const total = count
  const totalPages = Math.max(1, Math.ceil(total / perPage))
  const start = (page - 1) * perPage
  const end = Math.min(start + perPage, total)

  const getStateBadgeVariant = (state?: string) => {
    switch (state) {
      case 'completed':
        return 'success'
      case 'running':
        return 'default'
      case 'scheduled':
      case 'pending':
      case 'available':
        return 'secondary'
      case 'cancelled':
      case 'discarded':
        return 'destructive'
      case 'retryable':
        return 'destructive'
      default:
        return 'secondary'
    }
  }

  const formatDate = (dateString?: string) => {
    if (!dateString) return '-'
    return new Date(dateString).toLocaleString()
  }

  return (
    <div className="p-8">
      <div className="mb-8">
        <h1 className="font-bold text-3xl">Background Jobs</h1>
        <p className="mt-2 text-muted-foreground">View and monitor background job processing</p>
      </div>

      <div className="mb-4 flex gap-2">
        <select
          className="rounded-md border border-input bg-background px-3 py-2 text-sm"
          onChange={(e) => {
            setStateFilter(e.target.value)
            setPage(1)
          }}
          value={stateFilter}
        >
          <option value="">All States</option>
          <option value="available">Available</option>
          <option value="cancelled">Cancelled</option>
          <option value="completed">Completed</option>
          <option value="discarded">Discarded</option>
          <option value="pending">Pending</option>
          <option value="retryable">Retryable</option>
          <option value="running">Running</option>
          <option value="scheduled">Scheduled</option>
        </select>

        <select
          className="rounded-md border border-input bg-background px-3 py-2 text-sm"
          onChange={(e) => {
            setQueueFilter(e.target.value)
            setPage(1)
          }}
          value={queueFilter}
        >
          <option value="">All Queues</option>
          <option value="security">Security</option>
          <option value="notifications">Notifications</option>
          <option value="integrations">Integrations</option>
        </select>
      </div>

      <TablePane
        description="Background jobs are processed asynchronously by River workers"
        empty={jobs.length === 0}
        emptyMessage="No background jobs found"
        error={error ? 'Failed to load background jobs' : null}
        loading={!!isLoading}
      >
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>ID</TableHead>
              <TableHead>Kind</TableHead>
              <TableHead>Queue</TableHead>
              <TableHead>State</TableHead>
              <TableHead>Attempt</TableHead>
              <TableHead>Created</TableHead>
              <TableHead>Scheduled</TableHead>
              <TableHead>Last Error</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {jobs.map((job: JobResponse) => (
              <TableRow key={job.id}>
                <TableCell className="font-mono text-sm">{job.id}</TableCell>
                <TableCell className="font-mono text-xs">{job.kind}</TableCell>
                <TableCell>
                  <Badge variant="outline">{job.queue}</Badge>
                </TableCell>
                <TableCell>
                  <Badge variant={getStateBadgeVariant(job.state)}>{job.state}</Badge>
                </TableCell>
                <TableCell className="text-sm">
                  {job.attempt}/{job.max_attempts}
                </TableCell>
                <TableCell className="text-sm">{formatDate(job.created_at)}</TableCell>
                <TableCell className="text-sm">{formatDate(job.scheduled_at)}</TableCell>
                <TableCell
                  className="max-w-xs truncate text-muted-foreground text-sm"
                  title={job.last_error || ''}
                >
                  {job.last_error || '-'}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>

        {total > 0 && (
          <div className="mt-4 flex items-center justify-between">
            <div className="text-muted-foreground text-sm">
              Showing {start + 1}–{end} of {total} jobs
            </div>
            <div className="flex gap-2">
              <Button
                disabled={page === 1}
                onClick={() => setPage((p) => Math.max(1, p - 1))}
                variant="outline"
              >
                Previous
              </Button>
              <Button
                disabled={page === totalPages}
                onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                variant="outline"
              >
                Next
              </Button>
            </div>
          </div>
        )}
      </TablePane>
    </div>
  )
}
