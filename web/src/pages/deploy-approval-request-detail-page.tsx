import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { Check, Loader2, X } from 'lucide-react'
import type { ReactNode } from 'react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { type AuditLogRecord, AuditLogsPane } from '@/components/audit-logs-pane'
import { DeployApprovalRejectDialog } from '@/components/deploy-approval-reject-dialog'
import { DeployApprovalStatusBadge } from '@/components/deploy-approval-status-badge'
import { TimeAgo } from '@/components/time-ago'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { toast } from '@/components/ui/use-toast'
import { useStepUp } from '@/contexts/step-up-context'
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

type DetailRowProps = {
  label: string
  value: ReactNode
  valueClassName?: string
}

function DetailRow({ label, value, valueClassName }: DetailRowProps) {
  return (
    <div className="break-words text-sm">
      <span className="text-muted-foreground">{label}:</span>{' '}
      <span className={valueClassName}>{value ?? '—'}</span>
    </div>
  )
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

function toNotesPayload(notes: string): UpdateDeployApprovalRequestStatusRequest | undefined {
  const trimmed = notes.trim()
  return trimmed ? { notes: trimmed } : undefined
}

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: detail page with multiple sections
export function DeployApprovalRequestDetailPage() {
  const { id } = useParams({ from: '/deploy_approval_requests/$id' }) as {
    id: string
  }
  const queryClient = useQueryClient()
  const { handleStepUpError } = useStepUp()
  const [rejectDialogOpen, setRejectDialogOpen] = useState(false)

  const {
    data: request,
    isLoading: requestLoading,
    error: requestError,
  } = useQuery<DeployApprovalRequest, Error>({
    queryKey: ['deploy-approval-request', id],
    queryFn: () => api.get(`/api/v1/deploy-approval-requests/${id}`),
    retry: 1,
  })

  // Fetch app settings to get ci_org_slug for building pipeline URL
  const { data: appSettings } = useQuery<Record<string, { value: unknown; source: string }>, Error>(
    {
      queryKey: ['app-settings', request?.app],
      queryFn: () => api.get(`/api/v1/apps/${request?.app}/settings`),
      enabled: !!request?.app,
      retry: 1,
    }
  )

  // Extract CI metadata and build pipeline URL if available
  const { circleCIPipelineUrl, circleCIMetadata } = useMemo(() => {
    if (!request?.ci_metadata) {
      return { circleCIPipelineUrl: null, circleCIMetadata: null }
    }

    const metadata = extractCircleCIMetadata(request.ci_metadata)
    const ciOrgSlug = appSettings?.ci_org_slug?.value as string | undefined

    if (metadata.pipelineNumber && ciOrgSlug) {
      return {
        circleCIPipelineUrl: buildCircleCIPipelineUrl(ciOrgSlug, metadata.pipelineNumber),
        circleCIMetadata: metadata,
      }
    }

    return { circleCIPipelineUrl: null, circleCIMetadata: metadata }
  }, [request?.ci_metadata, appSettings?.ci_org_slug])

  const approveMutation = useMutation({
    mutationFn: (requestId: string) => approveDeployApprovalRequest(requestId, {}),
    onSuccess: () => {
      toast.success('Request approved')
      queryClient.invalidateQueries({
        queryKey: ['deploy-approval-request', id],
      })
      queryClient.invalidateQueries({ queryKey: ['deploy-approval-requests'] })
    },
  })

  const rejectMutation = useMutation({
    mutationFn: ({ requestId, notes }: { requestId: string; notes: string }) =>
      rejectDeployApprovalRequest(requestId, toNotesPayload(notes)),
    onSuccess: () => {
      toast.success('Request rejected')
      setRejectDialogOpen(false)
      queryClient.invalidateQueries({
        queryKey: ['deploy-approval-request', id],
      })
      queryClient.invalidateQueries({ queryKey: ['deploy-approval-requests'] })
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

  const handleApprove = () => {
    approveRequest(id).catch(() => {
      /* errors handled within approveRequest */
    })
  }

  const handleRejectClick = () => {
    setRejectDialogOpen(true)
  }

  const handleRejectSubmit = (notes: string) => {
    submitRejection(id, notes).catch(() => {
      /* errors handled within submitRejection */
    })
  }

  const [auditPageIndex, setAuditPageIndex] = useState(1)

  const {
    data: auditTableData,
    isLoading: auditTableLoading,
    error: auditTableError,
  } = useQuery<AuditLogsResponse, Error>({
    queryKey: ['deployApprovalRequestAuditLogs', id, auditPageIndex, DEFAULT_PER_PAGE],
    queryFn: () =>
      api.get(`/api/v1/admin/deploy-approval-requests/${id}/audit-logs?limit=${DEFAULT_PER_PAGE}`),
    enabled: !!request,
    placeholderData: keepPreviousData,
    refetchOnWindowFocus: true,
    staleTime: 0,
  })

  const auditLogs = (auditTableData?.logs ?? []) as AuditLogRecord[]
  const auditTotal = auditTableData?.total ?? 0
  const auditLimit = auditTableData?.limit ?? DEFAULT_PER_PAGE
  const currentAuditPage = auditTableData?.page ?? auditPageIndex
  const auditTotalPages = Math.max(1, Math.ceil(Math.max(auditTotal, 0) / auditLimit))
  const auditFirstRowIndex = auditTotal === 0 ? 0 : (currentAuditPage - 1) * auditLimit + 1
  const auditLastRowIndex = auditTotal === 0 ? 0 : auditFirstRowIndex + auditLogs.length - 1
  const auditLoading = auditTableLoading && auditLogs.length === 0
  const auditError = auditTableError ? auditTableError.message : null

  useEffect(() => {
    if (!auditTableData) {
      return
    }
    if (auditPageIndex !== currentAuditPage) {
      setAuditPageIndex(currentAuditPage)
      return
    }
    if (currentAuditPage > auditTotalPages) {
      setAuditPageIndex(auditTotalPages)
    }
  }, [auditTableData, auditPageIndex, currentAuditPage, auditTotalPages])

  const handleAuditPrevPage = () => {
    setAuditPageIndex((prev) => Math.max(1, prev - 1))
  }

  const handleAuditNextPage = () => {
    setAuditPageIndex((prev) => Math.min(auditTotalPages, prev + 1))
  }

  if (requestError) {
    return (
      <div className="space-y-4 p-8">
        <h1 className="font-semibold text-2xl">Deploy Approval Request</h1>
        <p className="text-destructive text-sm">Unable to load request: {requestError.message}</p>
      </div>
    )
  }

  const status = request?.status ?? 'unknown'
  const normalizedStatus = status.toLowerCase()
  const canApprove = normalizedStatus === 'pending'
  const canReject = normalizedStatus === 'pending' || normalizedStatus === 'approved'
  const approveDisabled = approveMutation.isPending || rejectMutation.isPending
  const rejectDisabled = rejectMutation.isPending || approveMutation.isPending

  return (
    <div className="space-y-6 p-8">
      {/* Header with app name and actions */}
      <div className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
        <div>
          <div className="flex items-center gap-3">
            <h1 className="font-semibold text-3xl">
              {requestLoading ? 'Loading…' : request?.app || 'Deploy Approval'}
            </h1>
            {request?.status && <DeployApprovalStatusBadge status={request.status} />}
          </div>
          <p className="mt-1 text-muted-foreground">
            {request?.message || 'Deploy approval request'}
          </p>
          <p className="font-mono text-muted-foreground text-sm">{id}</p>
        </div>
        <div className="flex flex-wrap items-center gap-2 md:justify-end">
          {canApprove && (
            <Button
              className="bg-green-600 hover:bg-green-700"
              disabled={approveDisabled || requestLoading}
              onClick={handleApprove}
            >
              {approveMutation.isPending ? (
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              ) : (
                <Check className="mr-2 h-4 w-4" />
              )}
              Approve
            </Button>
          )}
          {canReject && (
            <Button
              disabled={rejectDisabled || requestLoading}
              onClick={handleRejectClick}
              variant="destructive"
            >
              {rejectMutation.isPending ? (
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              ) : (
                <X className="mr-2 h-4 w-4" />
              )}
              Reject
            </Button>
          )}
        </div>
      </div>

      {/* Two-column layout for details */}
      <div className="grid gap-6 lg:grid-cols-2">
        {/* Deployment Info */}
        <Card>
          <CardHeader>
            <CardTitle>Deployment Info</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <DetailRow
              label="Git Commit"
              value={request?.git_commit_hash ?? '—'}
              valueClassName="font-mono"
            />
            <DetailRow
              label="Branch"
              value={request?.git_branch ?? '—'}
              valueClassName="font-mono"
            />
            {request?.pr_url && (
              <DetailRow
                label="Pull Request"
                value={
                  <a
                    className="text-link hover:underline"
                    href={request.pr_url}
                    rel="noopener noreferrer"
                    target="_blank"
                  >
                    {request.pr_url}
                  </a>
                }
              />
            )}
            {circleCIMetadata?.workflowId && (
              <DetailRow
                label="Workflow ID"
                value={circleCIMetadata.workflowId}
                valueClassName="font-mono text-xs"
              />
            )}
            {circleCIMetadata?.pipelineNumber && (
              <DetailRow label="Pipeline Number" value={circleCIMetadata.pipelineNumber} />
            )}
            {circleCIPipelineUrl && (
              <DetailRow
                label="Pipeline URL"
                value={
                  <a
                    className="text-link hover:underline"
                    href={circleCIPipelineUrl}
                    rel="noopener noreferrer"
                    target="_blank"
                  >
                    {circleCIPipelineUrl}
                  </a>
                }
              />
            )}
            <DetailRow label="Object URL" value={request?.object_url ?? '—'} />
            <DetailRow
              label="Build ID"
              value={request?.build_id ?? '—'}
              valueClassName="font-mono"
            />
            <DetailRow
              label="Release ID"
              value={request?.release_id ?? '—'}
              valueClassName="font-mono"
            />
            <DetailRow
              label="Process IDs"
              value={
                request?.process_ids && request.process_ids.length > 0
                  ? request.process_ids.join(', ')
                  : '—'
              }
              valueClassName="font-mono"
            />
          </CardContent>
        </Card>

        {/* Approval Workflow */}
        <Card>
          <CardHeader>
            <CardTitle>Approval Workflow</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <DetailRow
              label="Target Token"
              value={request?.target_api_token_name ?? request?.target_api_token_id ?? '—'}
            />
            <DetailRow
              label="Created By"
              value={
                request?.created_by_api_token_name ??
                formatUser(request?.created_by_name, request?.created_by_email)
              }
            />
            <DetailRow label="Created At" value={renderTime(request?.created_at)} />
            <DetailRow label="Updated At" value={renderTime(request?.updated_at)} />
            <DetailRow label="Expires At" value={renderTime(request?.approval_expires_at)} />
            <DetailRow
              label="Approved By"
              value={formatUser(request?.approved_by_name, request?.approved_by_email)}
            />
            <DetailRow label="Approved At" value={renderTime(request?.approved_at)} />
            {(request?.rejected_by_name || request?.rejected_by_email) && (
              <DetailRow
                label="Rejected By"
                value={formatUser(request.rejected_by_name, request.rejected_by_email)}
              />
            )}
            {request?.rejected_at && (
              <DetailRow label="Rejected At" value={renderTime(request.rejected_at)} />
            )}
            {request?.approval_notes && (
              <DetailRow label="Rejection Notes" value={request.approval_notes} />
            )}
          </CardContent>
        </Card>
      </div>

      {/* Executed Commands - table showing process IDs and commands */}
      {request?.exec_commands && (
        <Card>
          <CardHeader>
            <CardTitle>Executed Commands</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b">
                    <th className="pr-4 pb-2 text-left font-medium">Process ID</th>
                    <th className="pb-2 text-left font-medium">Command</th>
                  </tr>
                </thead>
                <tbody>
                  {Object.entries(request.exec_commands as Record<string, string>).map(
                    ([processId, command]) => (
                      <tr className="border-b last:border-0" key={processId}>
                        <td className="py-2 pr-4 align-top font-mono">{processId}</td>
                        <td className="py-2 font-mono">{command}</td>
                      </tr>
                    )
                  )}
                </tbody>
              </table>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Audit Logs */}
      <div data-testid="deploy-approval-audit-logs">
        <AuditLogsPane
          currentPage={currentAuditPage}
          disableNext={currentAuditPage >= auditTotalPages}
          disablePrevious={currentAuditPage <= 1}
          emptyMessage="No audit logs for this deploy approval request"
          error={auditError}
          firstRowIndex={auditFirstRowIndex}
          lastRowIndex={auditLastRowIndex}
          loading={auditLoading}
          logs={auditLogs}
          onNextPage={handleAuditNextPage}
          onPreviousPage={handleAuditPrevPage}
          title="Audit Logs"
          totalCount={auditTotal}
          totalPages={auditTotalPages}
        />
      </div>

      <DeployApprovalRejectDialog
        onOpenChange={setRejectDialogOpen}
        onSubmit={handleRejectSubmit}
        open={rejectDialogOpen}
        pending={rejectMutation.isPending}
        requestId={id}
      />
    </div>
  )
}
