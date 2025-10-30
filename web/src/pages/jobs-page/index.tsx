import { useQuery } from '@tanstack/react-query'
import { Eye } from 'lucide-react'
import { useState } from 'react'
import type { components } from '../../api/types.generated'
import { TablePane } from '../../components/table-pane'
import { Badge } from '../../components/ui/badge'
import { Button } from '../../components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '../../components/ui/dialog'
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
  const [selectedJob, setSelectedJob] = useState<JobResponse | null>(null)

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
              <TableHead className="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {jobs.map((job: JobResponse) => (
              <TableRow
                key={job.id}
                className="cursor-pointer"
                onClick={() => setSelectedJob(job)}
              >
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
                <TableCell
                  className="text-right"
                  onClick={(event) => {
                    event.stopPropagation()
                  }}
                >
                  <Button size="sm" variant="ghost">
                    <Eye className="h-4 w-4" />
                  </Button>
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

      <JobDetailDialog job={selectedJob} onClose={() => setSelectedJob(null)} />
    </div>
  )
}

type JobDetailDialogProps = {
  job: JobResponse | null
  onClose: () => void
}

function JobDetailDialog({ job, onClose }: JobDetailDialogProps) {
  return (
    <Dialog onOpenChange={(open) => !open && onClose()} open={Boolean(job)}>
      <DialogContent className="max-h-[80vh] max-w-2xl overflow-auto">
        <DialogHeader>
          <DialogTitle>Background Job Details</DialogTitle>
          <DialogDescription>
            Detailed information for the selected background job:
          </DialogDescription>
        </DialogHeader>
        {job ? (
          <div className="space-y-3 text-sm">
            <div>
              <span className="text-muted-foreground">ID:</span>{' '}
              <span className="font-mono">{job.id}</span>
            </div>
            <div>
              <span className="text-muted-foreground">Kind:</span>{' '}
              <span className="font-mono">{job.kind}</span>
            </div>
            <div>
              <span className="text-muted-foreground">Queue:</span> {job.queue}
            </div>
            <div>
              <span className="text-muted-foreground">State:</span>{' '}
              <Badge variant={getStateBadgeVariant(job.state)}>{job.state}</Badge>
            </div>
            <div>
              <span className="text-muted-foreground">Attempt:</span> {job.attempt}/
              {job.max_attempts}
            </div>
            <div>
              <span className="text-muted-foreground">Created At:</span>{' '}
              {job.created_at ? new Date(job.created_at).toISOString() : '-'}
            </div>
            <div>
              <span className="text-muted-foreground">Scheduled At:</span>{' '}
              {job.scheduled_at ? new Date(job.scheduled_at).toISOString() : '-'}
            </div>
            {job.attempted_at && (
              <div>
                <span className="text-muted-foreground">Attempted At:</span>{' '}
                {new Date(job.attempted_at).toISOString()}
              </div>
            )}
            {job.finalized_at && (
              <div>
                <span className="text-muted-foreground">Finalized At:</span>{' '}
                {new Date(job.finalized_at).toISOString()}
              </div>
            )}
            {job.last_error && (
              <div>
                <span className="text-muted-foreground">Last Error:</span>
                <pre className="mt-2 max-h-64 overflow-auto whitespace-pre-wrap rounded bg-muted p-2 text-xs wrap-break-word">
                  {job.last_error}
                </pre>
              </div>
            )}
            {job.errors && job.errors.length > 0 && (
              <div>
                <span className="text-muted-foreground">Errors:</span>
                <pre className="mt-2 max-h-64 overflow-auto whitespace-pre-wrap rounded bg-muted p-2 text-xs wrap-break-word">
                  {JSON.stringify(job.errors, null, 2)}
                </pre>
              </div>
            )}
            {job.args && (
              <div>
                <span className="text-muted-foreground">Arguments:</span>
                <pre className="mt-2 max-h-64 overflow-auto whitespace-pre-wrap rounded bg-muted p-2 text-xs wrap-break-word">
                  {typeof job.args === 'string' ? job.args : JSON.stringify(job.args, null, 2)}
                </pre>
              </div>
            )}
            <div className="mt-2 flex justify-end">
              <Button onClick={onClose} variant="outline">
                Close
              </Button>
            </div>
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  )
}

function getStateBadgeVariant(state?: string): 'success' | 'default' | 'secondary' | 'destructive' {
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
