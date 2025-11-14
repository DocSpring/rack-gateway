import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { Check, Loader2, X } from 'lucide-react'
import type { ReactNode } from 'react'
import { useCallback, useMemo, useState } from 'react'

import { type AuditLogRecord, AuditLogsPane } from '@/components/audit-logs-pane'
import { DeployApprovalRejectDialog } from '@/components/deploy-approval-reject-dialog'
import { DeployApprovalStatusBadge } from '@/components/deploy-approval-status-badge'
import { TimeAgo } from '@/components/time-ago'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { toast } from '@/components/ui/use-toast'
import { useStepUp } from '@/contexts/step-up-context'
import { useMutation } from '@/hooks/use-mutation'
import {
  type AuditLogsResponse,
  api,
  approveDeployApprovalRequest,
  type DeployApprovalRequest,
  rejectDeployApprovalRequest,
  type UpdateDeployApprovalRequestStatusRequest,
} from '@/lib/api'
import { buildCircleCIPipelineUrl, extractCircleCIMetadata } from '@/lib/ci-utils'
import { DEFAULT_PER_PAGE } from '@/lib/constants'
import { withAPIErrorMessage } from '@/lib/error-utils'
import { QUERY_KEYS } from '@/lib/query-keys'

const PLACEHOLDER_VALUE = '—'

type DetailRowProps = {
  label: string
  value: ReactNode
  valueClassName?: string
}

function DetailRow({ label, value, valueClassName }: DetailRowProps) {
  return (
    <div className="break-words text-sm">
      <span className="text-muted-foreground">{label}:</span>{' '}
      <span className={valueClassName}>{value ?? PLACEHOLDER_VALUE}</span>
    </div>
  )
}

function renderTime(value?: string | null): ReactNode {
  return value ? <TimeAgo date={value} /> : PLACEHOLDER_VALUE
}

function formatUser(name?: string | null, email?: string | null): string {
  if (name && email) {
    return `${name} (${email})`
  }
  return name ?? email ?? PLACEHOLDER_VALUE
}

function toNotesPayload(notes: string): UpdateDeployApprovalRequestStatusRequest | undefined {
  const trimmed = notes.trim()
  return trimmed ? { notes: trimmed } : undefined
}

function useDeployApprovalRequestData(id: string) {
  const {
    data: request,
    isLoading: requestLoading,
    error: requestError,
  } = useQuery<DeployApprovalRequest, Error>({
    queryKey: [...QUERY_KEYS.DEPLOY_APPROVAL_REQUEST, id],
    queryFn: () => api.get(`/api/v1/deploy-approval-requests/${id}`),
    retry: 1,
  })

  const { data: appSettings } = useQuery<Record<string, { value: unknown; source: string }>, Error>(
    {
      queryKey: ['app-settings', request?.app],
      queryFn: () => api.get(`/api/v1/apps/${request?.app}/settings`),
      enabled: Boolean(request?.app),
      retry: 1,
    }
  )

  const { circleCIPipelineUrl, circleCIMetadata } = useMemo(() => {
    if (!request?.ci_metadata) {
      return { circleCIPipelineUrl: null, circleCIMetadata: null }
    }

    const metadata = extractCircleCIMetadata(request.ci_metadata)
    const vcsProvider = appSettings?.vcs_provider?.value as string | undefined
    const vcsRepo = appSettings?.vcs_repo?.value as string | undefined

    if (metadata.pipelineNumber && vcsProvider && vcsRepo) {
      return {
        circleCIPipelineUrl: buildCircleCIPipelineUrl(
          vcsProvider,
          vcsRepo,
          metadata.pipelineNumber
        ),
        circleCIMetadata: metadata,
      }
    }

    return { circleCIPipelineUrl: null, circleCIMetadata: metadata }
  }, [appSettings?.vcs_provider, appSettings?.vcs_repo, request?.ci_metadata])

  return {
    request,
    requestLoading,
    requestError,
    circleCIPipelineUrl,
    circleCIMetadata,
  }
}

function useDeployApprovalAuditLogs(id: string, enabled: boolean) {
  const [pageIndex, setPageIndex] = useState(1)

  const { data, isLoading, error } = useQuery<AuditLogsResponse, Error>({
    queryKey: ['deployApprovalRequestAuditLogs', id, pageIndex, DEFAULT_PER_PAGE],
    queryFn: () =>
      api.get(`/api/v1/deploy-approval-requests/${id}/audit-logs?limit=${DEFAULT_PER_PAGE}`),
    enabled,
    placeholderData: keepPreviousData,
    refetchOnWindowFocus: true,
    staleTime: 0,
  })

  const logs = (data?.logs ?? []) as AuditLogRecord[]
  const total = data?.total ?? 0
  const limit = data?.limit ?? DEFAULT_PER_PAGE
  const currentPage = data?.page ?? pageIndex
  const totalPages = Math.max(1, Math.ceil(Math.max(total, 0) / limit))
  const firstRowIndex = total === 0 ? 0 : (currentPage - 1) * limit + 1
  const lastRowIndex = total === 0 ? 0 : firstRowIndex + logs.length - 1

  const goToPreviousPage = useCallback(() => {
    setPageIndex((prev) => Math.max(1, prev - 1))
  }, [])

  const goToNextPage = useCallback(() => {
    setPageIndex((prev) => Math.min(totalPages, prev + 1))
  }, [totalPages])

  return {
    auditLogs: logs,
    auditTotal: total,
    auditTotalPages: totalPages,
    auditFirstRowIndex: firstRowIndex,
    auditLastRowIndex: lastRowIndex,
    auditLoading: isLoading,
    auditError: error,
    auditPage: currentPage,
    goToPreviousPage,
    goToNextPage,
  }
}

type DeployActionsArgs = {
  id: string
  queryClient: ReturnType<typeof useQueryClient>
  handleStepUpError: ReturnType<typeof useStepUp>['handleStepUpError']
  closeRejectDialog: () => void
}

function useDeployApprovalActions({
  id,
  queryClient,
  handleStepUpError,
  closeRejectDialog,
}: DeployActionsArgs) {
  const approveMutation = useMutation({
    mutationFn: (requestId: string) => approveDeployApprovalRequest(requestId, {}),
    onSuccess: () => {
      toast.success('Request approved')
      queryClient.invalidateQueries({ queryKey: [...QUERY_KEYS.DEPLOY_APPROVAL_REQUEST, id] })
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.DEPLOY_APPROVAL_REQUESTS })
    },
  })

  const rejectMutation = useMutation({
    mutationFn: ({ requestId, notes }: { requestId: string; notes: string }) =>
      rejectDeployApprovalRequest(requestId, toNotesPayload(notes)),
    onSuccess: () => {
      toast.success('Request rejected')
      closeRejectDialog()
      queryClient.invalidateQueries({ queryKey: [...QUERY_KEYS.DEPLOY_APPROVAL_REQUEST, id] })
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.DEPLOY_APPROVAL_REQUESTS })
    },
  })

  const approveRequest = useCallback(
    async (requestId: string) => {
      try {
        await approveMutation.mutateAsync(requestId)
      } catch (err) {
        if (handleStepUpError(err, () => approveMutation.mutateAsync(requestId))) {
          return
        }
        withAPIErrorMessage(err, 'Failed to approve request', (message) =>
          toast.error('Approval failed', { description: message })
        )
      }
    },
    [approveMutation, handleStepUpError]
  )

  const submitRejection = useCallback(
    async (requestId: string, notes: string) => {
      try {
        await rejectMutation.mutateAsync({ requestId, notes })
      } catch (err) {
        if (handleStepUpError(err, () => rejectMutation.mutateAsync({ requestId, notes }))) {
          return
        }
        withAPIErrorMessage(err, 'Failed to reject request', (message) =>
          toast.error('Rejection failed', { description: message })
        )
      }
    },
    [handleStepUpError, rejectMutation]
  )

  return {
    approveRequest,
    submitRejection,
    approveMutationPending: approveMutation.isPending,
    rejectMutationPending: rejectMutation.isPending,
  }
}

export function DeployApprovalRequestDetailPage() {
  const { id } = useParams({ from: '/deploy-approval-requests/$id' }) as { id: string }
  const queryClient = useQueryClient()
  const { handleStepUpError } = useStepUp()
  const [rejectDialogOpen, setRejectDialogOpen] = useState(false)

  const { request, requestLoading, requestError, circleCIPipelineUrl, circleCIMetadata } =
    useDeployApprovalRequestData(id)

  const {
    auditLogs,
    auditTotal,
    auditTotalPages,
    auditFirstRowIndex,
    auditLastRowIndex,
    auditLoading,
    auditError,
    auditPage,
    goToPreviousPage,
    goToNextPage,
  } = useDeployApprovalAuditLogs(id, Boolean(request))

  const { approveRequest, submitRejection, approveMutationPending, rejectMutationPending } =
    useDeployApprovalActions({
      id,
      queryClient,
      handleStepUpError,
      closeRejectDialog: () => setRejectDialogOpen(false),
    })

  const handleApprove = useCallback(() => {
    approveRequest(id).catch(() => {
      /* handled in hook */
    })
  }, [approveRequest, id])

  const handleRejectSubmit = useCallback(
    (notes: string) => {
      submitRejection(id, notes).catch(() => {
        /* handled in hook */
      })
    },
    [id, submitRejection]
  )

  if (requestError) {
    return (
      <div className="space-y-4">
        <h1 className="font-semibold text-2xl">Deploy Approval Request</h1>
        <p className="text-destructive text-sm">Unable to load request: {requestError.message}</p>
      </div>
    )
  }

  if (requestLoading || !request) {
    return (
      <div className="flex h-full items-center justify-center p-8">
        <Loader2 className="size-5 animate-spin" />
      </div>
    )
  }

  const targetTokenDisplay =
    request.target_api_token_name ?? request.target_api_token_id ?? PLACEHOLDER_VALUE

  return (
    <div className="space-y-8 p-8">
      <div className="flex flex-col gap-2 md:flex-row md:items-start md:justify-between">
        <div className="space-y-2">
          <h1 className="font-semibold text-3xl">Deploy Approval Request</h1>
          <DetailRow label="App" value={request.app ?? PLACEHOLDER_VALUE} />
          <DetailRow label="Target Token" value={targetTokenDisplay} />
          <DetailRow
            label="Created"
            value={renderTime(request.created_at)}
            valueClassName="text-muted-foreground"
          />
          <DetailRow label="Status" value={<DeployApprovalStatusBadge status={request.status} />} />
          <DetailRow
            label="Created by"
            value={formatUser(request.created_by_name, request.created_by_email)}
          />
          {circleCIPipelineUrl ? (
            <DetailRow
              label="CI Pipeline"
              value={
                <a
                  className="text-primary"
                  href={circleCIPipelineUrl}
                  rel="noreferrer"
                  target="_blank"
                >
                  CircleCI Pipeline #{circleCIMetadata?.pipelineNumber}
                </a>
              }
            />
          ) : (
            <DetailRow label="CI Pipeline" value={PLACEHOLDER_VALUE} />
          )}
        </div>
        <div className="flex flex-wrap gap-2">
          <Button
            disabled={approveMutationPending || request.status !== 'pending'}
            onClick={handleApprove}
            variant="secondary"
          >
            {approveMutationPending ? (
              <Loader2 className="mr-2 size-4 animate-spin" />
            ) : (
              <Check className="mr-2 size-4" />
            )}
            Approve Request
          </Button>
          <Button
            disabled={rejectMutationPending || request.status !== 'pending'}
            onClick={() => setRejectDialogOpen(true)}
            variant="destructive"
          >
            <X className="mr-2 size-4" /> Reject Request
          </Button>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Request Details</CardTitle>
        </CardHeader>
        <CardContent className="space-y-2">
          <DetailRow
            label="Message"
            value={request.message || 'No message provided'}
            valueClassName="text-sm"
          />
          <DetailRow label="Branch" value={request.git_branch ?? PLACEHOLDER_VALUE} />
          <DetailRow label="Git Commit" value={request.git_commit_hash ?? PLACEHOLDER_VALUE} />
          <DetailRow label="Release ID" value={request.release_id ?? PLACEHOLDER_VALUE} />
          <DetailRow label="Build ID" value={request.build_id ?? PLACEHOLDER_VALUE} />
        </CardContent>
      </Card>

      <div data-testid="deploy-approval-audit-logs">
        <AuditLogsPane
          currentPage={auditPage}
          disableNext={auditPage >= auditTotalPages}
          disablePrevious={auditPage <= 1}
          emptyMessage="No audit logs for this request"
          error={auditError?.message ?? null}
          firstRowIndex={auditFirstRowIndex}
          lastRowIndex={auditLastRowIndex}
          loading={auditLoading}
          logs={auditLogs}
          onNextPage={goToNextPage}
          onPreviousPage={goToPreviousPage}
          title="Audit Logs"
          totalCount={auditTotal}
          totalPages={auditTotalPages}
        />
      </div>

      <DeployApprovalRejectDialog
        onOpenChange={setRejectDialogOpen}
        onSubmit={handleRejectSubmit}
        open={rejectDialogOpen}
        pending={rejectMutationPending}
        requestId={id}
      />
    </div>
  )
}
