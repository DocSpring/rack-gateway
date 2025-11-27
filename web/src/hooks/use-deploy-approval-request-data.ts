import { useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'

import { api, type DeployApprovalRequest } from '@/lib/api'
import { buildCircleCIPipelineUrl, extractCircleCIMetadata } from '@/lib/ci-utils'

export function useDeployApprovalRequestData(id: string) {
  const {
    data: request,
    isLoading: requestLoading,
    error: requestError,
  } = useQuery<DeployApprovalRequest, Error>({
    queryKey: ['deploy-approval-request', id],
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
