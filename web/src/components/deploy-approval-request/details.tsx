import { Check, Clock, Loader2, X } from 'lucide-react'
import type { ReactNode } from 'react'

import { DeployApprovalStatusBadge } from '@/components/deploy-approval-status-badge'
import { ExpiryTime } from '@/components/expiry-time'
import { TimeAgo } from '@/components/time-ago'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import type { DeployApprovalRequest } from '@/lib/api'

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

function formatCreatedBy(request: DeployApprovalRequest): string {
  // If created by an API token, show the token name
  if (request.created_by_api_token_name) {
    return `${request.created_by_api_token_name} (API Token)`
  }
  // Otherwise show the user
  return formatUser(request.created_by_name, request.created_by_email)
}

type RequestHeaderProps = {
  request: DeployApprovalRequest
  circleCIPipelineUrl: string | null
  circleCIMetadata: { pipelineNumber?: string | number } | null
}

export function RequestHeader({
  request,
  circleCIPipelineUrl,
  circleCIMetadata,
}: RequestHeaderProps) {
  const targetTokenDisplay =
    request.target_api_token_name ?? request.target_api_token_id ?? PLACEHOLDER_VALUE

  return (
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
      <DetailRow label="Created by" value={formatCreatedBy(request)} />
      {request.approval_expires_at && (
        <DetailRow
          label="Expires"
          value={<ExpiryTime date={request.approval_expires_at} />}
          valueClassName="font-mono"
        />
      )}
      {circleCIPipelineUrl ? (
        <DetailRow
          label="CI Pipeline"
          value={
            <a className="text-primary" href={circleCIPipelineUrl} rel="noreferrer" target="_blank">
              CircleCI Pipeline #{circleCIMetadata?.pipelineNumber}
            </a>
          }
        />
      ) : (
        <DetailRow label="CI Pipeline" value={PLACEHOLDER_VALUE} />
      )}
    </div>
  )
}

type ActionButtonsProps = {
  approveMutationPending: boolean
  extendMutationPending: boolean
  rejectMutationPending: boolean
  requestStatus: string
  onApprove: () => void
  onExtend: () => void
  onReject: () => void
}

export function ActionButtons({
  approveMutationPending,
  extendMutationPending,
  rejectMutationPending,
  requestStatus,
  onApprove,
  onExtend,
  onReject,
}: ActionButtonsProps) {
  const isPending = requestStatus === 'pending'
  const isApproved = requestStatus === 'approved'

  return (
    <div className="flex flex-wrap gap-2">
      {isPending && (
        <Button disabled={approveMutationPending} onClick={onApprove} variant="secondary">
          {approveMutationPending ? (
            <Loader2 className="mr-2 size-4 animate-spin" />
          ) : (
            <Check className="mr-2 size-4" />
          )}
          Approve Request
        </Button>
      )}
      {isApproved && (
        <Button disabled={extendMutationPending} onClick={onExtend} variant="secondary">
          {extendMutationPending ? (
            <Loader2 className="mr-2 size-4 animate-spin" />
          ) : (
            <Clock className="mr-2 size-4" />
          )}
          Extend Approval
        </Button>
      )}
      {(isPending || isApproved) && (
        <Button disabled={rejectMutationPending} onClick={onReject} variant="destructive">
          {rejectMutationPending ? (
            <Loader2 className="mr-2 size-4 animate-spin" />
          ) : (
            <X className="mr-2 size-4" />
          )}
          Reject Request
        </Button>
      )}
    </div>
  )
}

type RequestDetailsCardProps = {
  request: DeployApprovalRequest
}

export function RequestDetailsCard({ request }: RequestDetailsCardProps) {
  return (
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
        <DetailRow label="Branch" value={request.git_branch ?? '—'} />
        <DetailRow label="Git Commit" value={request.git_commit_hash ?? '—'} />
        <DetailRow label="Release ID" value={request.release_id ?? '—'} />
        <DetailRow label="Build ID" value={request.build_id ?? '—'} />
      </CardContent>
    </Card>
  )
}

export function LoadingState() {
  return (
    <div className="flex h-full items-center justify-center p-8">
      <Loader2 className="size-5 animate-spin" />
    </div>
  )
}

export function ErrorState({ error }: { error: Error }) {
  return (
    <div className="space-y-4">
      <h1 className="font-semibold text-2xl">Deploy Approval Request</h1>
      <p className="text-destructive text-sm">Unable to load request: {error.message}</p>
    </div>
  )
}
