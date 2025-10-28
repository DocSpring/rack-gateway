import { Lock } from 'lucide-react'
import { TimeAgo } from '@/components/time-ago'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

type LockedNoticeProps = {
  lockedAt?: string | null
  lockedByName?: string | null
  lockedByEmail?: string | null
  lockedReason?: string | null
}

export function LockedNotice({
  lockedAt,
  lockedByName,
  lockedByEmail,
  lockedReason,
}: LockedNoticeProps) {
  if (!lockedAt) {
    return null
  }
  return (
    <Card className="border-orange-500/50" data-testid="user-locked-notice">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-orange-400">
          <Lock className="h-5 w-5" />
          Locked
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-2 text-sm">
        <div>
          <span className="font-medium">Locked at:</span> <TimeAgo date={lockedAt} />
        </div>
        {lockedByName && (
          <div>
            <span className="font-medium">Locked by:</span> {lockedByName}
            {lockedByEmail && ` (${lockedByEmail})`}
          </div>
        )}
        {lockedReason && (
          <div>
            <span className="font-medium">Reason:</span> {lockedReason}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
