import { useQueryClient } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { useCallback, useState } from 'react'

import { AuditLogsPane } from '@/components/audit-logs-pane'
import { DeployApprovalRejectDialog } from '@/components/deploy-approval-reject-dialog'
import {
  ActionButtons,
  ErrorState,
  LoadingState,
  RequestDetailsCard,
  RequestHeader,
} from '@/components/deploy-approval-request/details'
import { useStepUp } from '@/contexts/step-up-context'
import { useDeployApprovalActions } from '@/hooks/use-deploy-approval-actions'
import { useDeployApprovalAuditLogs } from '@/hooks/use-deploy-approval-audit-logs'
import { useDeployApprovalRequestData } from '@/hooks/use-deploy-approval-request-data'

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

  const {
    approveRequest,
    extendRequest,
    submitRejection,
    approveMutationPending,
    extendMutationPending,
    rejectMutationPending,
  } = useDeployApprovalActions({
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

  const handleExtend = useCallback(() => {
    extendRequest(id).catch(() => {
      /* handled in hook */
    })
  }, [extendRequest, id])

  const handleRejectSubmit = useCallback(
    (notes: string) => {
      submitRejection(id, notes).catch(() => {
        /* handled in hook */
      })
    },
    [id, submitRejection]
  )

  const handleRejectClick = useCallback(() => {
    setRejectDialogOpen(true)
  }, [])

  if (requestError) return <ErrorState error={requestError} />
  if (requestLoading || !request) return <LoadingState />

  return (
    <div className="space-y-8 p-8">
      <RequestHeader
        circleCIMetadata={circleCIMetadata}
        circleCIPipelineUrl={circleCIPipelineUrl}
        request={request}
      />

      <RequestDetailsCard request={request} />

      <ActionButtons
        approveMutationPending={approveMutationPending}
        extendMutationPending={extendMutationPending}
        onApprove={handleApprove}
        onExtend={handleExtend}
        onReject={handleRejectClick}
        rejectMutationPending={rejectMutationPending}
        requestStatus={request.status}
      />

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
