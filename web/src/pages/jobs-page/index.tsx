import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Eye, RotateCw, X } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'react-hot-toast'
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

type JobListResponse = components['schemas']['handlers.JobListResponse']
type JobResponse = components['schemas']['handlers.JobResponse']

export function JobsPage() {
  return <JobsPageInner />
}

function JobsPageInner() {
  const [stateFilter, setStateFilter] = useState<string>('')
  const [queueFilter, setQueueFilter] = useState<string>('')
  const [kindFilter, setKindFilter] = useState<string>('')
  const [selectedJob, setSelectedJob] = useState<JobResponse | null>(null)
  const queryClient = useQueryClient()

  // Track cursor stack for back navigation
  const [cursors, setCursors] = useState<string[]>([])
  const currentCursor = cursors.at(-1)

  const { data, isLoading, error } = useQuery({
    queryKey: ['jobs', stateFilter, queueFilter, kindFilter, currentCursor],
    queryFn: async () => {
      const params = new URLSearchParams()
      if (stateFilter) {
        params.append('state', stateFilter)
      }
      if (queueFilter) {
        params.append('queue', queueFilter)
      }
      if (kindFilter) {
        params.append('kind', kindFilter)
      }
      if (currentCursor) {
        params.append('after', currentCursor)
      }
      const queryString = params.toString()
      const url = `/api/v1/jobs${queryString ? `?${queryString}` : ''}`
      const response = await api.get<JobListResponse>(url)
      return response
    },
  })

  const deleteMutation = useMutation({
    mutationFn: async (jobId: number) => {
      await api.delete(`/api/v1/jobs/${jobId}`)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['jobs'] })
      toast.success('Job deleted successfully')
    },
    onError: () => {
      toast.error('Failed to delete job')
    },
  })

  const retryMutation = useMutation({
    mutationFn: async (jobId: number) => {
      await api.post(`/api/v1/jobs/${jobId}/retry`)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['jobs'] })
      toast.success('Job queued for retry')
    },
    onError: () => {
      toast.error('Failed to retry job')
    },
  })

  const jobs = data?.jobs || []
  const pageInfo = data?.page_info

  // Cursor-only pagination (no count for River jobs)
  const hasNextPage = pageInfo?.has_next_page ?? false
  const hasPrevPage = pageInfo?.has_previous_page ?? false

  const formatDate = (dateString?: string) => {
    if (!dateString) return '-'
    return new Date(dateString).toLocaleString()
  }

  const canDeleteJob = (state?: string): boolean => {
    // Only allow deletion for failed/retrying jobs
    // Completed, running, cancelled, and discarded jobs should not be deleted
    return state === 'retryable' || state === 'scheduled' || state === 'available'
  }

  const canRetryJob = (state?: string): boolean => {
    // Allow retry for failed, cancelled, and discarded jobs
    return state === 'retryable' || state === 'cancelled' || state === 'discarded'
  }

  return (
    <div className="p-8">
      <div className="mb-8">
        <h1 className="font-bold text-3xl">Background Jobs</h1>
        <p className="mt-2 text-muted-foreground">View and monitor background job processing</p>
      </div>

      <div className="mb-4 flex flex-wrap gap-2">
        <select
          className="rounded-md border border-input bg-background px-3 py-2 text-sm"
          onChange={(e) => {
            setStateFilter(e.target.value)
            setCursors([])
          }}
          value={stateFilter}
        >
          <option value="">All States</option>
          <option value="available">Available</option>
          <option value="cancelled">Cancelled</option>
          <option value="completed">Completed</option>
          <option value="discarded">Discarded</option>
          <option value="retryable">Retryable</option>
          <option value="running">Running</option>
          <option value="scheduled">Scheduled</option>
        </select>

        <select
          className="rounded-md border border-input bg-background px-3 py-2 text-sm"
          onChange={(e) => {
            setQueueFilter(e.target.value)
            setCursors([])
          }}
          value={queueFilter}
        >
          <option value="">All Queues</option>
          <option value="default">Default</option>
          <option value="security">Security</option>
          <option value="notifications">Notifications</option>
          <option value="integrations">Integrations</option>
        </select>

        <select
          className="rounded-md border border-input bg-background px-3 py-2 text-sm"
          onChange={(e) => {
            setKindFilter(e.target.value)
            setCursors([])
          }}
          value={kindFilter}
        >
          <option value="">All Job Types</option>
          <option value="audit:anchor_writer">Audit Anchor</option>
          <option value="deploy:approval_notify">Deploy Approval</option>
          <option value="email:failed_login">Failed Login</option>
          <option value="email:failed_mfa">Failed MFA</option>
          <option value="email:mfa_auto_lock">MFA Auto Lock</option>
          <option value="email:send_single">Send Email</option>
          <option value="email:welcome">Welcome Email</option>
          <option value="slack:audit_event">Slack Audit</option>
          <option value="slack:deploy_approval">Slack Deploy</option>
          <option value="github:post_pr_comment">GitHub Comment</option>
          <option value="circleci:approve_job">CircleCI Approval</option>
        </select>

        {(stateFilter || queueFilter || kindFilter) && (
          <Button
            onClick={() => {
              setStateFilter('')
              setQueueFilter('')
              setKindFilter('')
              setCursors([])
            }}
            size="sm"
            variant="outline"
          >
            Clear Filters
          </Button>
        )}
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
              <TableRow className="cursor-pointer" key={job.id} onClick={() => setSelectedJob(job)}>
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
                  <div className="flex justify-end gap-1">
                    <Button size="sm" variant="ghost">
                      <Eye className="h-4 w-4" />
                    </Button>
                    <Button
                      disabled={!(job.id && canRetryJob(job.state))}
                      onClick={(event) => {
                        event.stopPropagation()
                        if (job.id && canRetryJob(job.state)) {
                          retryMutation.mutate(job.id)
                        }
                      }}
                      size="sm"
                      variant="ghost"
                    >
                      <RotateCw className="h-4 w-4" />
                    </Button>
                    <Button
                      disabled={!(job.id && canDeleteJob(job.state))}
                      onClick={(event) => {
                        event.stopPropagation()
                        if (job.id && canDeleteJob(job.state)) {
                          deleteMutation.mutate(job.id)
                        }
                      }}
                      size="sm"
                      variant="ghost"
                    >
                      <X className="h-4 w-4" />
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>

        {jobs.length > 0 && (
          <div className="mt-4 flex items-center justify-between">
            <div className="text-muted-foreground text-sm">Showing {jobs.length} jobs</div>
            <div className="flex gap-2">
              <Button
                disabled={!hasPrevPage}
                onClick={() => {
                  if (cursors.length > 0) {
                    setCursors((prev) => prev.slice(0, -1))
                  }
                }}
                size="sm"
                variant="outline"
              >
                Previous
              </Button>
              <Button
                disabled={!hasNextPage}
                onClick={() => {
                  if (pageInfo?.end_cursor) {
                    setCursors((prev) => [...prev, pageInfo.end_cursor || ''])
                  }
                }}
                size="sm"
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
                <pre className="wrap-break-word mt-2 max-h-64 overflow-auto whitespace-pre-wrap rounded bg-muted p-2 text-xs">
                  {job.last_error}
                </pre>
              </div>
            )}
            {job.errors && job.errors.length > 0 && (
              <div>
                <span className="text-muted-foreground">Errors:</span>
                <pre className="wrap-break-word mt-2 max-h-64 overflow-auto whitespace-pre-wrap rounded bg-muted p-2 text-xs">
                  {JSON.stringify(job.errors, null, 2)}
                </pre>
              </div>
            )}
            {job.args && (
              <div>
                <span className="text-muted-foreground">Arguments:</span>
                <pre className="wrap-break-word mt-2 max-h-64 overflow-auto whitespace-pre-wrap rounded bg-muted p-2 text-xs">
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
