import type { AuditLogEntry } from '@/lib/api'

export type AuditLogRecord = AuditLogEntry

export type AuditLogBadgeAppearance = {
  variant: 'default' | 'secondary' | 'destructive' | 'outline'
  className?: string
}
