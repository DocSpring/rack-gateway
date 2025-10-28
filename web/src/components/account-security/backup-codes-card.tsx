import { TimeAgo } from '@/components/time-ago'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import type { MFAStatusResponse } from '@/lib/api'

type BackupCodesCardProps = {
  backupSummary: MFAStatusResponse['backup_codes'] | undefined
  recentBackupCodes: string[] | null
  onRegenerate: () => void
  regeneratePending: boolean
  onDownloadCodes: (codes: string[]) => void
}

export function BackupCodesCard({
  backupSummary,
  recentBackupCodes,
  onRegenerate,
  regeneratePending,
  onDownloadCodes,
}: BackupCodesCardProps) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Backup Codes</CardTitle>
      </CardHeader>
      <CardContent className="flex-1 space-y-6 pb-2">
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1">
            <p className="text-muted-foreground text-xs uppercase tracking-wide">Unused codes</p>
            <p className="font-semibold text-2xl">{backupSummary?.unused ?? 0}</p>
          </div>
          {backupSummary?.last_generated_at ? (
            <div className="space-y-1">
              <p className="text-muted-foreground text-xs uppercase tracking-wide">
                Last generated
              </p>
              <p className="text-sm">
                <TimeAgo date={backupSummary.last_generated_at} />
              </p>
            </div>
          ) : null}
        </div>
      </CardContent>
      <CardFooter className="mt-auto flex flex-wrap gap-2">
        <Button disabled={regeneratePending} onClick={onRegenerate}>
          Regenerate backup codes
        </Button>
        {recentBackupCodes && recentBackupCodes.length > 0 ? (
          <Button onClick={() => onDownloadCodes(recentBackupCodes)} variant="outline">
            Download latest codes
          </Button>
        ) : null}
      </CardFooter>
    </Card>
  )
}
