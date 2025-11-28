import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Navigate, useNavigate } from '@tanstack/react-router'
import { Check, Loader2, Timer, X } from 'lucide-react'
import type { KeyboardEvent } from 'react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { DeployApprovalRejectDialog } from '@/components/deploy-approval-reject-dialog'
import { DeployApprovalStatusBadge } from '@/components/deploy-approval-status-badge'
import { ExpiryTime } from '@/components/expiry-time'
import { PageLayout } from '@/components/page-layout'
import { TablePane } from '@/components/table-pane'
import { TimeAgo } from '@/components/time-ago'
import { Button } from '@/components/ui/button'
import { NativeSelect } from '@/components/ui/native-select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { toast } from '@/components/ui/use-toast'
import { UserMetaCell } from '@/components/user-meta-cell'
import { UuidCell } from '@/components/uuid-cell'
import { useAuth } from '@/contexts/auth-context'
import { useStepUp } from '@/contexts/step-up-context'
import { useMutation } from '@/hooks/use-mutation'
import {
  approveDeployApprovalRequest,
  type DeployApprovalRequest,
  listDeployApprovalRequests,
  rejectDeployApprovalRequest,
  type UpdateDeployApprovalRequestStatusRequest,
} from '@/lib/api'
import { DEFAULT_PER_PAGE } from '@/lib/constants'
import { withAPIErrorMessage } from '@/lib/error-utils'

type StatusFilter = 'all' | 'pending' | 'approved' | 'rejected' | 'consumed'

const STATUS_OPTIONS: { value: StatusFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'pending', label: 'Pending' },
  { value: 'approved', label: 'Approved' },
  { value: 'rejected', label: 'Rejected' },
  { value: 'consumed', label: 'Consumed' },
]

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

export function DeployApprovalRequestsPage() {
  const { user, isLoading: isAuthLoading } = useAuth()
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [rejectDialogOpen, setRejectDialogOpen] = useState(false)
  const [rejectRequestId, setRejectRequestId] = useState<string | null>(null)
  const [approvingId, setApprovingId] = useState<string | null>(null)
  const queryClient = useQueryClient()
  const { handleStepUpError } = useStepUp()
  const navigate = useNavigate()

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
    mutationFn: (id: string) => approveDeployApprovalRequest(id, {}),
    onSuccess: (_data, id) => {
      toast.success(`Request ${id} was approved`)
      setApprovingId(null)
      queryClient.invalidateQueries({ queryKey })
    },
    onError: () => {
      setApprovingId(null)
    },
  })

  const rejectMutation = useMutation({
    mutationFn: ({ id, notes }: { id: string; notes: string }) =>
      rejectDeployApprovalRequest(id, toNotesPayload(notes)),
    onSuccess: (_data, { id }) => {
      toast.success(`Request ${id} was rejected`)
      setRejectDialogOpen(false)
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
    async (id: string) => {
      setApprovingId(id)
      try {
        await approveMutation.mutateAsync(id)
      } catch (err) {
        if (handleStepUpError(err, () => approveMutation.mutateAsync(id))) {
          return
        }
        setApprovingId(null)
        withAPIErrorMessage(err, 'Failed to approve request', (message) =>
          toast.error('Approval failed', { description: message })
        )
      }
    },
    [approveMutation, handleStepUpError]
  )

  const submitRejection = useCallback(
    async (id: string, notes: string) => {
      try {
        await rejectMutation.mutateAsync({ id, notes })
      } catch (err) {
        if (handleStepUpError(err, () => rejectMutation.mutateAsync({ id, notes }))) {
          return
        }
        withAPIErrorMessage(err, 'Failed to reject request', (message) =>
          toast.error('Rejection failed', { description: message })
        )
      }
    },
    [handleStepUpError, rejectMutation]
  )

  const handleApprove = useCallback(
    (id: string) => {
      approveRequest(id).catch(() => {
        /* errors handled within approveRequest */
      })
    },
    [approveRequest]
  )

  const handleRejectClick = (request: DeployApprovalRequest) => {
    setRejectRequestId(request.public_id)
    setRejectDialogOpen(true)
  }

  const handleRejectSubmit = (notes: string) => {
    if (!rejectRequestId) return
    submitRejection(rejectRequestId, notes).catch(() => {
      /* errors handled within submitRejection */
    })
  }

  // Deploy approvals are always available now, no need to check for feature flag
  // Just ensure user is authenticated
  if (!(isAuthLoading || user)) {
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
                <TableHead>Git Commit</TableHead>
                <TableHead>Branch</TableHead>
                <TableHead>API Token</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Decided By</TableHead>
                <TableHead>Created</TableHead>
                <TableHead>Expires</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {visibleRequests.map((request) => (
                <DeployApprovalRequestRow
                  approveDisabled={approveDisabled}
                  approvingId={approvingId}
                  key={request.public_id}
                  onApprove={handleApprove}
                  onReject={handleRejectClick}
                  onSelect={(req) =>
                    navigate({
                      to: '/deploy-approval-requests/$id',
                      params: { id: req.public_id },
                    })
                  }
                  rejectDisabled={rejectDisabled}
                  rejectingId={rejectRequestId}
                  request={request}
                />
              ))}
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

      <DeployApprovalRejectDialog
        onOpenChange={setRejectDialogOpen}
        onSubmit={handleRejectSubmit}
        open={rejectDialogOpen}
        pending={rejectMutation.isPending}
        requestId={rejectRequestId ?? ''}
      />
    </PageLayout>
  )
}

type DeployApprovalRequestRowProps = {
  request: DeployApprovalRequest
  approveDisabled: boolean
  approvingId: string | null
  rejectDisabled: boolean
  rejectingId: string | null
  onApprove: (id: string) => void
  onReject: (request: DeployApprovalRequest) => void
  onSelect: (request: DeployApprovalRequest) => void
}

function DeployApprovalRequestRow({
  request,
  approveDisabled,
  approvingId,
  rejectDisabled,
  rejectingId,
  onApprove,
  onReject,
  onSelect,
}: DeployApprovalRequestRowProps) {
  const publicId = request.public_id
  const message = request.message
  const tokenName = request.target_api_token_name ?? 'Unknown token'
  const tokenId = request.target_api_token_id
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

  const shortCommit = request.git_commit_hash.substring(0, 7)
  const gitBranch = request.git_branch ?? '—'

  return (
    <TableRow
      className="cursor-pointer hover:bg-muted/40"
      key={publicId}
      onClick={handleRowClick}
      onKeyDown={handleKeyDown}
      role="button"
      tabIndex={0}
    >
      <TableCell>
        <UuidCell label="Request ID" uuid={publicId} />
      </TableCell>
      <TableCell className="max-w-xs truncate" title={message}>
        {message}
      </TableCell>
      <TableCell className="font-mono text-sm" title={request.git_commit_hash}>
        {shortCommit}
      </TableCell>
      <TableCell className="font-mono text-sm">{gitBranch}</TableCell>
      <TableCell>
        {tokenId ? (
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <span className="cursor-help text-sm">{tokenName}</span>
              </TooltipTrigger>
              <TooltipContent>
                <span className="font-mono">{tokenId}</span>
              </TooltipContent>
            </Tooltip>
          </TooltipProvider>
        ) : (
          <span className="text-sm">{tokenName}</span>
        )}
      </TableCell>
      <TableCell>
        <DeployApprovalStatusBadge status={status} />
      </TableCell>
      <TableCell>
        <UserMetaCell email={decidedBy?.email} name={decidedBy?.name} />
      </TableCell>
      <TableCell>
        <TimeAgo date={request.created_at} />
      </TableCell>

      <TableCell>
        <div className="flex items-center gap-1 text-sm">
          {showExpiresAt ? (
            <>
              <Timer className="h-4 w-4 text-muted-foreground" />
              <ExpiryTime date={request.approval_expires_at} />
            </>
          ) : (
            '—'
          )}
        </div>
      </TableCell>
      <TableCell>
        <div className="flex justify-end gap-1">
          {canApprove && (
            <Button
              aria-label={`Approve request ${publicId}`}
              disabled={approveDisabled}
              onClick={(event) => {
                event.stopPropagation()
                onApprove(publicId)
              }}
              size="sm"
              variant="ghost"
            >
              {approvingId === publicId ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <Check className="h-4 w-4 text-green-600" />
              )}
            </Button>
          )}
          {canReject && (
            <Button
              aria-label={`Reject request ${publicId}`}
              disabled={rejectDisabled}
              onClick={(event) => {
                event.stopPropagation()
                onReject(request)
              }}
              size="sm"
              variant="ghost"
            >
              {rejectingId === publicId ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <X className="h-4 w-4 text-red-600" />
              )}
            </Button>
          )}
        </div>
      </TableCell>
    </TableRow>
  )
}
