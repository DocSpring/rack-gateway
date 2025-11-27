import type { useQueryClient } from '@tanstack/react-query'
import { useCallback } from 'react'

import { toast } from '@/components/ui/use-toast'
import type { useStepUp } from '@/contexts/step-up-context'
import { useMutation } from '@/hooks/use-mutation'
import {
  approveDeployApprovalRequest,
  extendDeployApprovalRequest,
  rejectDeployApprovalRequest,
  type UpdateDeployApprovalRequestStatusRequest,
} from '@/lib/api'
import { withAPIErrorMessage } from '@/lib/error-utils'
import { QUERY_KEYS } from '@/lib/query-keys'

function toNotesPayload(notes: string): UpdateDeployApprovalRequestStatusRequest | undefined {
  const trimmed = notes.trim()
  return trimmed ? { notes: trimmed } : undefined
}

type DeployActionsArgs = {
  id: string
  queryClient: ReturnType<typeof useQueryClient>
  handleStepUpError: ReturnType<typeof useStepUp>['handleStepUpError']
  closeRejectDialog: () => void
}

export function useDeployApprovalActions({
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

  const extendMutation = useMutation({
    mutationFn: (requestId: string) => extendDeployApprovalRequest(requestId),
    onSuccess: () => {
      toast.success('Approval extended')
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

  const extendRequest = useCallback(
    async (requestId: string) => {
      try {
        await extendMutation.mutateAsync(requestId)
      } catch (err) {
        if (handleStepUpError(err, () => extendMutation.mutateAsync(requestId))) {
          return
        }
        withAPIErrorMessage(err, 'Failed to extend approval', (message) =>
          toast.error('Extension failed', { description: message })
        )
      }
    },
    [extendMutation, handleStepUpError]
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
    extendRequest,
    submitRejection,
    approveMutationPending: approveMutation.isPending,
    extendMutationPending: extendMutation.isPending,
    rejectMutationPending: rejectMutation.isPending,
  }
}
