import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Navigate } from '@tanstack/react-router'
import type { VariantProps } from 'class-variance-authority'
import { Check, Eye, Loader2, MoreVertical, Timer, X } from 'lucide-react'
import type { ChangeEvent, KeyboardEvent, ReactNode } from 'react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { PageLayout } from '@/components/page-layout'
import { TablePane } from '@/components/table-pane'
import { TimeAgo } from '@/components/time-ago'
import { Badge, type badgeVariants } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Label } from '@/components/ui/label'
import { NativeSelect } from '@/components/ui/native-select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { toast } from '@/components/ui/use-toast'
import { UserMetaCell } from '@/components/user-meta-cell'
import { useAuth } from '@/contexts/auth-context'
import { useStepUp } from '@/contexts/step-up-context'
import {
  approveDeployApprovalRequest,
  type DeployApprovalRequest,
  listDeployApprovalRequests,
  rejectDeployApprovalRequest,
  type UpdateDeployApprovalRequestStatusRequest,
} from '@/lib/api'
import { DEFAULT_PER_PAGE } from '@/lib/constants'
import { getErrorMessage } from '@/lib/error-utils'

type StatusFilter = 'all' | 'pending' | 'approved' | 'rejected' | 'consumed'

const STATUS_OPTIONS: { value: StatusFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'pending', label: 'Pending' },
  { value: 'approved', label: 'Approved' },
  { value: 'rejected', label: 'Rejected' },
  { value: 'consumed', label: 'Consumed' },
]

type BadgeVariant = NonNullable<VariantProps<typeof badgeVariants>['variant']>

const STATUS_BADGE_VARIANTS: Record<string, BadgeVariant> = {
  pending: 'outline',
  approved: 'success',
  consumed: 'secondary',
  rejected: 'destructive',
}

function statusBadge(status: string) {
  const normalized = status.toLowerCase()
  const variant = STATUS_BADGE_VARIANTS[normalized] ?? 'secondary'
  return <Badge variant={variant}>{status}</Badge>
}

function toNotesPayload(notes: string): UpdateDeployApprovalRequestStatusRequest | undefined {
  const trimmed = notes.trim()
  return trimmed ? { notes: trimmed } : undefined
}

type PaginationResult<T> = {
  page: number
  setPage: (value: number | ((prev: number) => number)) => void
  total: number
  totalPages: number
  start: number
  end: number
  items: T[]
}

function usePagination<T>(items: T[], perPage: number): PaginationResult<T> {
  const [page, setPage] = useState(1)
  const total = items.length
  const totalPages = Math.max(1, Math.ceil(total / perPage))
  const start = (page - 1) * perPage
  const end = Math.min(start + perPage, total)
  const visibleItems = useMemo(() => items.slice(start, end), [items, start, end])

  useEffect(() => {
    if (total === 0) {
      if (page !== 1) {
        setPage(1)
      }
      return
    }
    if (page > totalPages) {
      setPage(totalPages)
    }
  }, [page, total, totalPages])

  return {
    page,
    setPage,
    total,
    totalPages,
    start,
    end,
    items: visibleItems,
  }
}

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: keep consolidated for now.
export function DeployApprovalRequestsPage() {
  const { user, isLoading: isAuthLoading } = useAuth()
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [rejectRequest, setRejectRequest] = useState<DeployApprovalRequest | null>(null)
  const [rejectNotes, setRejectNotes] = useState('')
  const [selectedRequest, setSelectedRequest] = useState<DeployApprovalRequest | null>(null)
  const queryClient = useQueryClient()
  const { handleStepUpError } = useStepUp()

  const queryKey = useMemo(() => ['deploy-approval-requests', statusFilter], [statusFilter])

  const { data, isLoading, isError, error } = useQuery({
    queryKey,
    queryFn: () => {
      const params = statusFilter === 'all' ? undefined : { status: statusFilter }
      return listDeployApprovalRequests(params)
    },
    staleTime: 0,
    refetchOnMount: 'always',
    refetchOnReconnect: 'always',
    refetchOnWindowFocus: true,
  })

  const approveMutation = useMutation({
    mutationFn: (id: number) => approveDeployApprovalRequest(id, {}),
    onSuccess: (_data, id) => {
      toast.success(`Request ${id} was approved`)
      queryClient.invalidateQueries({ queryKey })
    },
  })

  const rejectMutation = useMutation({
    mutationFn: ({ id, notes }: { id: number; notes: string }) =>
      rejectDeployApprovalRequest(id, toNotesPayload(notes)),
    onSuccess: (_data, { id }) => {
      toast.success(`Request ${id} was rejected`)
      setRejectRequest(null)
      setRejectNotes('')
      queryClient.invalidateQueries({ queryKey })
    },
  })

  const requests = data?.deploy_approval_requests ?? []

  const {
    page,
    setPage,
    total,
    totalPages,
    start,
    end,
    items: visibleRequests,
  } = usePagination(requests, DEFAULT_PER_PAGE)

  const isEmpty = !isLoading && total === 0

  const approveDisabled = approveMutation.isPending || rejectMutation.isPending
  const rejectDisabled = rejectMutation.isPending || approveMutation.isPending

  const approveRequest = useCallback(
    async (id: number) => {
      try {
        await approveMutation.mutateAsync(id)
      } catch (err) {
        if (handleStepUpError(err, () => approveMutation.mutateAsync(id))) {
          return
        }
        const description = getErrorMessage(err, 'Failed to approve request')
        toast.error('Approval failed', { description })
      }
    },
    [approveMutation, handleStepUpError]
  )

  const submitRejection = useCallback(
    async (id: number, notes: string) => {
      try {
        await rejectMutation.mutateAsync({ id, notes })
      } catch (err) {
        if (handleStepUpError(err, () => rejectMutation.mutateAsync({ id, notes }))) {
          return
        }
        const description = getErrorMessage(err, 'Failed to reject request')
        toast.error('Rejection failed', { description })
      }
    },
    [handleStepUpError, rejectMutation]
  )

  const handleApprove = useCallback(
    (id: number) => {
      approveRequest(id).catch(() => {
        /* errors handled within approveRequest */
      })
    },
    [approveRequest]
  )

  const handleRejectClick = (request: DeployApprovalRequest) => {
    setRejectRequest(request)
    setRejectNotes('')
  }

  // Redirect if deploy approvals are disabled (must be after all hooks)
  // Wait for auth to load before checking to avoid false redirects on page refresh
  if (!(isAuthLoading || user?.deploy_approvals_enabled)) {
    return <Navigate replace to="/" />
  }

  return (
    <PageLayout description="Manual review queue for CI/CD deploys" title="Deploy Approvals">
      <div className="space-y-6">
        <TablePane
          empty={isEmpty}
          emptyMessage={`No ${statusFilter === 'all' ? '' : `${statusFilter} `}deploy approval requests found.`}
          error={isError ? error : null}
          headerRight={
            <div className="flex items-center gap-2">
              <NativeSelect
                className="h-9 w-40"
                onChange={(event) => {
                  setStatusFilter(event.target.value as StatusFilter)
                  setPage(1)
                }}
                value={statusFilter}
              >
                {STATUS_OPTIONS.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </NativeSelect>
            </div>
          }
          loading={isLoading}
          title="Deploy Approval Requests"
        >
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>ID</TableHead>
                <TableHead>Message</TableHead>
                <TableHead>App</TableHead>
                <TableHead>Target Token</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Decided By</TableHead>
                <TableHead>Created</TableHead>
                <TableHead>Updated</TableHead>
                <TableHead>Expires</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {visibleRequests.map((request) =>
                request.id == null ? null : (
                  <DeployApprovalRequestRow
                    approveDisabled={approveDisabled}
                    approvePending={approveMutation.isPending}
                    key={request.id}
                    onApprove={handleApprove}
                    onReject={handleRejectClick}
                    onSelect={setSelectedRequest}
                    rejectDisabled={rejectDisabled}
                    rejectPending={rejectMutation.isPending}
                    request={request}
                  />
                )
              )}
            </TableBody>
          </Table>
          {total > 0 && (
            <div className="mt-4 flex items-center justify-between">
              <div className="text-muted-foreground text-sm">
                Showing {start + 1}–{end} of {total} requests
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
                  disabled={page >= totalPages}
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

      <Dialog
        onOpenChange={(open) => (open ? null : setRejectRequest(null))}
        open={rejectRequest !== null}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Reject deploy approval request</DialogTitle>
            <DialogDescription>
              Provide an optional reason for rejecting request {rejectRequest?.id ?? '—'}.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="reject-notes">Reason (optional)</Label>
            <Textarea
              id="reject-notes"
              onChange={(event: ChangeEvent<HTMLTextAreaElement>) =>
                setRejectNotes(event.target.value)
              }
              placeholder="Provide additional context for the requester"
              rows={4}
              value={rejectNotes}
            />
          </div>
          <DialogFooter>
            <Button onClick={() => setRejectRequest(null)} variant="outline">
              Cancel
            </Button>
            <Button
              disabled={rejectMutation.isPending}
              onClick={() => {
                if (!rejectRequest || rejectRequest.id == null) {
                  return
                }
                submitRejection(rejectRequest.id, rejectNotes).catch(() => {
                  /* errors handled within submitRejection */
                })
              }}
              variant="destructive"
            >
              {rejectMutation.isPending ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <X className="h-4 w-4" />
              )}
              Reject request
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        onOpenChange={(open) => (open ? null : setSelectedRequest(null))}
        open={selectedRequest != null}
      >
        <DialogContent className="max-h-[80vh] max-w-2xl overflow-auto">
          <DialogHeader>
            <DialogTitle>Deploy approval request details</DialogTitle>
            <DialogDescription>
              Review the full metadata for the selected request.
            </DialogDescription>
          </DialogHeader>
          {selectedRequest && (
            <div className="space-y-3 text-sm">
              <DetailRow label="Message" value={selectedRequest.message ?? '—'} />
              <DetailRow label="Status" value={selectedRequest.status ?? '—'} />
              <DetailRow
                label="Target Token"
                value={
                  selectedRequest.target_api_token_name ??
                  selectedRequest.target_api_token_id ??
                  '—'
                }
              />
              <DetailRow label="Created" value={renderTime(selectedRequest.created_at)} />
              <DetailRow label="Updated" value={renderTime(selectedRequest.updated_at)} />
              <DetailRow label="Expires" value={renderTime(selectedRequest.approval_expires_at)} />

              <DetailRow
                label="Promoted By Token"
                value={selectedRequest.release_promoted_by_api_token_id ?? '—'}
              />
              <DetailRow
                label="Created By"
                value={
                  selectedRequest.created_by_api_token_name ??
                  formatUser(selectedRequest.created_by_name, selectedRequest.created_by_email)
                }
              />
              <DetailRow
                label="Approved By"
                value={formatUser(
                  selectedRequest.approved_by_name,
                  selectedRequest.approved_by_email
                )}
              />
              <DetailRow label="Approved At" value={renderTime(selectedRequest.approved_at)} />
              <DetailRow
                label="Rejected By"
                value={formatUser(
                  selectedRequest.rejected_by_name,
                  selectedRequest.rejected_by_email
                )}
              />
              <DetailRow label="Rejected At" value={renderTime(selectedRequest.rejected_at)} />
              <DetailRow label="Reviewer Notes" value={selectedRequest.approval_notes ?? '—'} />

              <DetailRow label="App" value={selectedRequest.app ?? '—'} />
              <DetailRow label="Release ID" value={selectedRequest.release_id ?? '—'} />
              <DetailRow
                label="Release Created"
                value={renderTime(selectedRequest.release_created_at)}
              />
              <DetailRow
                label="Release Promoted"
                value={renderTime(selectedRequest.release_promoted_at)}
              />
            </div>
          )}
          <div className="mt-2 flex justify-end">
            <Button onClick={() => setSelectedRequest(null)} variant="outline">
              Close
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </PageLayout>
  )
}

type DeployApprovalRequestRowProps = {
  request: DeployApprovalRequest
  approveDisabled: boolean
  approvePending: boolean
  rejectDisabled: boolean
  rejectPending: boolean
  onApprove: (id: number) => void
  onReject: (request: DeployApprovalRequest) => void
  onSelect: (request: DeployApprovalRequest) => void
}

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Row component consolidates UI logic
function DeployApprovalRequestRow({
  request,
  approveDisabled,
  approvePending,
  rejectDisabled,
  rejectPending,
  onApprove,
  onReject,
  onSelect,
}: DeployApprovalRequestRowProps) {
  const id = request.id
  if (id == null) {
    return null
  }

  const message = request.message ?? '(no message provided)'
  const tokenLabel =
    request.target_api_token_name ??
    (request.target_api_token_id != null ? `Token ${request.target_api_token_id}` : 'Unknown token')
  const tokenIdLabel = request.target_api_token_id ?? '—'
  const status = request.status ?? 'unknown'
  const normalizedStatus = status.toLowerCase()
  const canApprove = normalizedStatus === 'pending'
  const canReject = normalizedStatus === 'pending' || normalizedStatus === 'approved'

  const showExpiresAt = request.approval_expires_at && canReject
  const decidedBy = (() => {
    if (request.approved_by_email || request.approved_by_name) {
      return {
        name: request.approved_by_name ?? undefined,
        email: request.approved_by_email ?? undefined,
      }
    }
    if (request.rejected_by_email || request.rejected_by_name) {
      return {
        name: request.rejected_by_name ?? undefined,
        email: request.rejected_by_email ?? undefined,
      }
    }
    return null
  })()

  const handleRowClick = () => onSelect(request)
  const handleKeyDown = (event: KeyboardEvent<HTMLTableRowElement>) => {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault()
      onSelect(request)
    }
  }

  return (
    <TableRow
      className="cursor-pointer hover:bg-muted/40"
      key={id}
      onClick={handleRowClick}
      onKeyDown={handleKeyDown}
      role="button"
      tabIndex={0}
    >
      <TableCell className="font-mono text-sm">{id}</TableCell>
      <TableCell className="max-w-xs truncate" title={message}>
        {message}
      </TableCell>
      <TableCell className="font-mono text-sm">{request.app ?? '—'}</TableCell>
      <TableCell>
        <div className="flex flex-col text-sm">
          <span>{tokenLabel}</span>
          <span className="text-muted-foreground text-sm">ID {tokenIdLabel}</span>
        </div>
      </TableCell>
      <TableCell>{statusBadge(status)}</TableCell>
      <TableCell>
        <UserMetaCell email={decidedBy?.email} name={decidedBy?.name} />
      </TableCell>
      <TableCell>
        <TimeAgo date={request.created_at} />
      </TableCell>
      <TableCell>
        <TimeAgo date={request.updated_at} />
      </TableCell>

      <TableCell>
        <div className="flex items-center gap-1 text-sm">
          {showExpiresAt ? (
            <>
              <Timer className="h-4 w-4 text-muted-foreground" />
              <TimeAgo date={request.approval_expires_at} />
            </>
          ) : (
            '—'
          )}
        </div>
      </TableCell>
      <TableCell>
        <div className="flex justify-end">
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                aria-label={`Actions for request ${id}`}
                onClick={(event) => event.stopPropagation()}
                size="sm"
                variant="ghost"
              >
                <MoreVertical className="h-4 w-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem
                onClick={(event) => {
                  event.stopPropagation()
                  onSelect(request)
                }}
              >
                <Eye className="h-4 w-4" />
                View Details
              </DropdownMenuItem>
              {(canApprove || canReject) && <DropdownMenuSeparator />}
              {canApprove && (
                <DropdownMenuItem
                  disabled={approveDisabled}
                  onClick={(event) => {
                    event.stopPropagation()
                    onApprove(id)
                  }}
                >
                  {approvePending ? (
                    <Loader2 className="h-4 w-4 animate-spin" />
                  ) : (
                    <Check className="h-4 w-4" />
                  )}
                  Approve
                </DropdownMenuItem>
              )}
              {canReject && (
                <DropdownMenuItem
                  disabled={rejectDisabled}
                  onClick={(event) => {
                    event.stopPropagation()
                    onReject(request)
                  }}
                  variant="destructive"
                >
                  {rejectPending ? (
                    <Loader2 className="h-4 w-4 animate-spin" />
                  ) : (
                    <X className="h-4 w-4" />
                  )}
                  Reject
                </DropdownMenuItem>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </TableCell>
    </TableRow>
  )
}

type DetailRowProps = {
  label: string
  value: ReactNode
}

function renderTime(value?: string | null): ReactNode {
  return value ? <TimeAgo date={value} /> : '—'
}

function formatUser(name?: string | null, email?: string | null): string {
  if (name && email) {
    return `${name} (${email})`
  }
  return name ?? email ?? '—'
}

function DetailRow({ label, value }: DetailRowProps) {
  return (
    <div className="break-words text-sm">
      <span className="text-muted-foreground">{label}:</span> {value ?? '—'}
    </div>
  )
}
