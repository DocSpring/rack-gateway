import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { VariantProps } from 'class-variance-authority'
import { CheckCircle2, Loader2, ShieldX, Timer } from 'lucide-react'
import type { ChangeEvent } from 'react'
import { useMemo, useState } from 'react'
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
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
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
import {
  approveDeployRequest,
  type DeployRequest,
  listDeployRequests,
  rejectDeployRequest,
  type UpdateDeployRequestStatusRequest,
} from '@/lib/api'

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

function toNotesPayload(notes: string): UpdateDeployRequestStatusRequest | undefined {
  const trimmed = notes.trim()
  return trimmed ? { notes: trimmed } : undefined
}

export function DeployRequestsPage() {
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [rejectRequest, setRejectRequest] = useState<DeployRequest | null>(null)
  const [rejectNotes, setRejectNotes] = useState('')
  const queryClient = useQueryClient()

  const queryKey = useMemo(() => ['deploy-requests', statusFilter], [statusFilter])

  const { data, isLoading, isError, error } = useQuery({
    queryKey,
    queryFn: () => {
      const params = statusFilter === 'all' ? undefined : { status: statusFilter }
      return listDeployRequests(params)
    },
  })

  const approveMutation = useMutation({
    mutationFn: (id: number) => approveDeployRequest(id, {}),
    onSuccess: (_data, id) => {
      toast({ title: 'Deploy request approved', description: `Request ${id} was approved.` })
      queryClient.invalidateQueries({ queryKey })
    },
    onError: (err: Error) => {
      toast.error('Approval failed', { description: err.message })
    },
  })

  const rejectMutation = useMutation({
    mutationFn: ({ id, notes }: { id: number; notes: string }) =>
      rejectDeployRequest(id, toNotesPayload(notes)),
    onSuccess: (_data, { id }) => {
      toast({ title: 'Deploy request rejected', description: `Request ${id} was rejected.` })
      setRejectRequest(null)
      setRejectNotes('')
      queryClient.invalidateQueries({ queryKey })
    },
    onError: (err: Error) => {
      toast.error('Rejection failed', { description: err.message })
    },
  })

  const requests = data?.deploy_requests ?? []

  const isEmpty = !isLoading && requests.length === 0

  const approveDisabled = approveMutation.isPending || rejectMutation.isPending
  const rejectDisabled = rejectMutation.isPending || approveMutation.isPending

  const handleRejectClick = (request: DeployRequest) => {
    setRejectRequest(request)
    setRejectNotes('')
  }

  return (
    <PageLayout description="Manual review queue for CI/CD deploys" title="Deploy Approvals">
      <div className="space-y-6">
        <TablePane
          empty={isEmpty}
          emptyMessage={`No ${statusFilter} deploy requests found`}
          error={isError ? error : null}
          headerRight={
            <div className="flex items-center gap-2">
              <Select
                onValueChange={(value) => setStatusFilter(value as StatusFilter)}
                value={statusFilter}
              >
                <SelectTrigger className="w-40">
                  <SelectValue placeholder="Filter status" />
                </SelectTrigger>
                <SelectContent>
                  {STATUS_OPTIONS.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          }
          loading={isLoading}
          title="Deploy Requests"
        >
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>ID</TableHead>
                <TableHead>Message</TableHead>
                <TableHead>Target Token</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Created</TableHead>
                <TableHead>Updated</TableHead>
                <TableHead>Expires</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {requests.map((request) =>
                request.id == null ? null : (
                  <DeployRequestRow
                    approveDisabled={approveDisabled}
                    approvePending={approveMutation.isPending}
                    key={request.id}
                    onApprove={(id) => approveMutation.mutate(id)}
                    onReject={handleRejectClick}
                    rejectDisabled={rejectDisabled}
                    rejectPending={rejectMutation.isPending}
                    request={request}
                  />
                )
              )}
            </TableBody>
          </Table>
        </TablePane>
      </div>

      <Dialog
        onOpenChange={(open) => (open ? null : setRejectRequest(null))}
        open={rejectRequest !== null}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Reject deploy request</DialogTitle>
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
                rejectMutation.mutate({ id: rejectRequest.id, notes: rejectNotes })
              }}
              variant="destructive"
            >
              {rejectMutation.isPending ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <ShieldX className="h-4 w-4" />
              )}
              Reject request
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </PageLayout>
  )
}

type DeployRequestRowProps = {
  request: DeployRequest
  approveDisabled: boolean
  approvePending: boolean
  rejectDisabled: boolean
  rejectPending: boolean
  onApprove: (id: number) => void
  onReject: (request: DeployRequest) => void
}

function DeployRequestRow({
  request,
  approveDisabled,
  approvePending,
  rejectDisabled,
  rejectPending,
  onApprove,
  onReject,
}: DeployRequestRowProps) {
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

  return (
    <TableRow key={id}>
      <TableCell className="font-mono text-sm">{id}</TableCell>
      <TableCell className="max-w-xs truncate" title={message}>
        {message}
      </TableCell>
      <TableCell>
        <div className="flex flex-col text-sm">
          <span>{tokenLabel}</span>
          <span className="text-muted-foreground text-sm">ID {tokenIdLabel}</span>
        </div>
      </TableCell>
      <TableCell>{statusBadge(status)}</TableCell>
      <TableCell>
        <TimeAgo date={request.created_at} />
      </TableCell>
      <TableCell>
        <TimeAgo date={request.updated_at} />
      </TableCell>
      <TableCell>
        <div className="flex items-center gap-1 text-sm">
          {request.approval_expires_at ? (
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
        <div className="flex justify-end gap-2">
          {canApprove && (
            <Button disabled={approveDisabled} onClick={() => onApprove(id)} variant="outline">
              {approvePending ? (
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              ) : (
                <CheckCircle2 className="mr-2 h-4 w-4" />
              )}
              Approve
            </Button>
          )}
          {canReject && (
            <Button
              disabled={rejectDisabled}
              onClick={() => onReject(request)}
              variant="destructive"
            >
              {rejectPending ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <ShieldX className="h-4 w-4" />
              )}
              Reject
            </Button>
          )}
        </div>
      </TableCell>
    </TableRow>
  )
}
