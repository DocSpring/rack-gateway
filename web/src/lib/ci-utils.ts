/**
 * CI Provider Utilities
 *
 * Functions for building CI provider URLs and handling CI metadata
 */

/**
 * Extract CircleCI metadata from generic CI metadata
 */
export function extractCircleCIMetadata(ciMetadata?: { [key: string]: unknown }): {
  workflowId?: string
  pipelineNumber?: string | number
} {
  if (!ciMetadata) {
    return {}
  }

  return {
    workflowId: ciMetadata.workflow_id as string | undefined,
    pipelineNumber: ciMetadata.pipeline_number as string | number | undefined,
  }
}

/**
 * Map VCS provider name to CircleCI VCS short code
 */
function getVCSShortCode(vcsProvider: string): string {
  const provider = vcsProvider.toLowerCase()
  if (provider === 'github') return 'gh'
  if (provider === 'bitbucket') return 'bb'
  return provider // fallback to provider name
}

/**
 * Build CircleCI pipeline URL
 * Format: https://app.circleci.com/pipelines/{vcs}/{org}/{repo}/{pipeline_number}
 *
 * @param vcsProvider - VCS provider (e.g., "github", "bitbucket")
 * @param vcsRepo - Repository in org/repo format (e.g., "DocSpring/docspring")
 * @param pipelineNumber - Pipeline number from CI metadata
 */
export function buildCircleCIPipelineUrl(
  vcsProvider: string,
  vcsRepo: string,
  pipelineNumber: string | number
): string {
  const vcsShortCode = getVCSShortCode(vcsProvider)
  return `https://app.circleci.com/pipelines/${vcsShortCode}/${vcsRepo}/${pipelineNumber}`
}
