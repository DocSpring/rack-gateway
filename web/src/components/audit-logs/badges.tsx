import type { AuditLogBadgeAppearance } from '@/components/audit-logs/types'
import { MAX_RESOURCE_LABEL_LENGTH } from '@/components/audit-logs/utils'
import { Badge } from '@/components/ui/badge'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'

export function LabelBadge({ label }: { label: string }) {
  const needsTruncate = label.length > MAX_RESOURCE_LABEL_LENGTH
  const shortText = needsTruncate ? `${label.slice(0, MAX_RESOURCE_LABEL_LENGTH - 3)}...` : label
  const content = (
    <Badge
      className="border border-border bg-muted font-mono text-muted-foreground"
      variant="outline"
    >
      {shortText || '-'}
    </Badge>
  )
  if (!needsTruncate) {
    return content
  }
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>{content}</TooltipTrigger>
        <TooltipContent>
          <span className="font-mono">{label}</span>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}

export function getStatusBadgeAppearance(status?: string): AuditLogBadgeAppearance {
  switch (status) {
    case 'success':
      return {
        variant: 'default',
        className: 'bg-green-600 text-white hover:bg-green-700',
      }
    case 'failed':
    case 'error':
    case 'blocked':
    case 'denied':
      return { variant: 'destructive' }
    default:
      return { variant: 'outline' }
  }
}

export function getActionTypeBadgeAppearance(type?: string): AuditLogBadgeAppearance {
  switch (type) {
    case 'auth':
      return {
        variant: 'outline',
        className: 'bg-blue-600 text-white border border-border',
      }
    case 'users':
      return {
        variant: 'outline',
        className: 'bg-purple-600 text-white border border-border',
      }
    case 'tokens':
      return {
        variant: 'outline',
        className: 'bg-amber-600 text-white border border-border',
      }
    case 'convox':
      return {
        variant: 'outline',
        className: 'bg-slate-700 text-white border border-border',
      }
    default:
      return {
        variant: 'outline',
        className: 'bg-muted text-muted-foreground border border-border',
      }
  }
}

export function getResourceTypeBadgeAppearance(type?: string): AuditLogBadgeAppearance {
  switch (type) {
    case 'app':
      return {
        variant: 'outline',
        className: 'bg-indigo-600 text-white border border-border',
      }
    case 'rack':
      return {
        variant: 'outline',
        className: 'bg-emerald-600 text-white border border-border',
      }
    case 'env':
      return {
        variant: 'outline',
        className: 'bg-orange-500 text-white border border-border',
      }
    case 'api_token':
      return {
        variant: 'outline',
        className: 'bg-rose-600 text-white border border-border',
      }
    case 'user':
    case 'auth':
      return { variant: 'default', className: 'bg-blue-600 text-white' }
    default:
      return {
        variant: 'outline',
        className: 'bg-muted text-muted-foreground border border-border',
      }
  }
}
