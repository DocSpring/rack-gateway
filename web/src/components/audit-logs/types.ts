import type { DbAuditLogAggregated } from '@/api/schemas'
import type { AuditLogEntry } from '@/lib/api'

export type AuditLogRecord = AuditLogEntry | DbAuditLogAggregated

export interface AuditLogBadgeAppearance {
  variant: 'default' | 'secondary' | 'destructive' | 'outline'
  className?: string
}
