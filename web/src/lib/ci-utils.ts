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
 * Build CircleCI pipeline URL
 * Format: https://app.circleci.com/pipelines/{ci_org_slug}/{pipeline_number}
 *
 * @param ciOrgSlug - Organization slug (e.g., "gh/DocSpring/docspring" or "github/DocSpring/docspring")
 * @param pipelineNumber - Pipeline number from CI metadata
 */
export function buildCircleCIPipelineUrl(
  ciOrgSlug: string,
  pipelineNumber: string | number
): string {
  return `https://app.circleci.com/pipelines/${ciOrgSlug}/${pipelineNumber}`
}
