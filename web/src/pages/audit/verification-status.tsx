import { useQuery } from '@tanstack/react-query'
import { ShieldAlert, ShieldCheck, ShieldQuestionMark } from 'lucide-react'

import type { DbAuditVerificationStatus, HandlersAuditVerificationResponse } from '@/api/schemas'
import { TimeAgo } from '@/components/time-ago'
import { Card, CardContent } from '@/components/ui/card'
import { api } from '@/lib/api'
import { cn } from '@/lib/utils'

function headline(status: DbAuditVerificationStatus, anchored: boolean): string {
  switch (status.result) {
    case 'passed':
      return anchored ? 'Audit chain verified' : 'Audit chain verified (no external anchors)'
    case 'failed':
      return `Audit chain verification failed: ${status.failure_count} integrity failures`
    default:
      return "Audit chain verification couldn't run"
  }
}

// The outcome of the gateway's hourly check of the audit hash chain and its S3 anchors.
export function AuditVerificationSummary({
  status,
  anchored,
}: {
  // null before the first run (the generated type only allows the field to be missing)
  status?: DbAuditVerificationStatus | null
  // whether the gateway anchors the chain in S3 (AUDIT_ANCHOR_BUCKET)
  anchored: boolean
}) {
  if (!status) {
    return (
      <Card className="mb-6 py-4">
        <CardContent className="flex items-center gap-3 text-sm">
          <ShieldQuestionMark className="size-5 shrink-0 text-muted-foreground" />
          <span>
            Audit chain not verified yet. The gateway checks the hash chain and its anchors every
            hour.
          </span>
        </CardContent>
      </Card>
    )
  }

  const passed = status.result === 'passed'
  const Icon = passed ? ShieldCheck : ShieldAlert
  return (
    <Card className={cn('mb-6 py-4', !passed && 'border-destructive/50')}>
      <CardContent className="space-y-2 text-sm">
        <div className="flex items-center gap-3">
          <Icon className={cn('size-5 shrink-0', passed ? 'text-green-600' : 'text-destructive')} />
          <span className="font-medium">{headline(status, anchored)}</span>
        </div>
        {!anchored && (
          <p className="text-muted-foreground">
            Without S3 anchors (AUDIT_ANCHOR_BUCKET), verification can&apos;t detect events removed
            from the end of the chain, or the whole chain.
          </p>
        )}
        <p className="text-muted-foreground">
          Checked <TimeAgo date={status.checked_at} />
          {status.failing_since && (
            <>
              {' '}
              · failing since <TimeAgo date={status.failing_since} />
            </>
          )}
          {status.full_checked_at && (
            <>
              {' '}
              · last full check <TimeAgo date={status.full_checked_at} />
            </>
          )}
        </p>
        <details>
          <summary className="cursor-pointer text-muted-foreground">Report</summary>
          <pre className="mt-2 max-h-96 overflow-auto whitespace-pre-wrap font-mono text-xs">
            {status.summary}
          </pre>
        </details>
      </CardContent>
    </Card>
  )
}

// Shown on the gateway-wide audit log, for users who can read it.
export function AuditVerificationCard() {
  const { data } = useQuery({
    queryKey: ['audit-verification'],
    queryFn: () => api.get<HandlersAuditVerificationResponse>('/api/v1/audit-logs/verification'),
  })
  if (!data) {
    return null
  }
  return <AuditVerificationSummary anchored={data.anchors_configured} status={data.status} />
}
