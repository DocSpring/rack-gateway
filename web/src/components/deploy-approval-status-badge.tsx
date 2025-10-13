import { Badge } from '@/components/ui/badge'

type BadgeVariant = 'success' | 'destructive' | 'secondary' | 'outline'

const STATUS_BADGE_VARIANTS: Record<string, BadgeVariant | 'deployed'> = {
  pending: 'outline',
  approved: 'success',
  consumed: 'secondary',
  deployed: 'deployed',
  rejected: 'destructive',
}

export function DeployApprovalStatusBadge({ status }: { status: string }) {
  const normalized = status.toLowerCase()
  const config = STATUS_BADGE_VARIANTS[normalized] ?? 'secondary'

  if (config === 'deployed') {
    return <Badge className="bg-purple-600 text-white">{status}</Badge>
  }

  return <Badge variant={config as BadgeVariant}>{status}</Badge>
}
