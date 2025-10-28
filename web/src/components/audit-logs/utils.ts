import type { AuditLogRecord } from '@/components/audit-logs/types'

export const MAX_RESOURCE_LABEL_LENGTH = 23

function safeParseDetails(details: string | undefined | null): Record<string, unknown> {
  if (!details) {
    return {}
  }
  try {
    return JSON.parse(details) as Record<string, unknown>
  } catch {
    return {}
  }
}

export function getAPITokenInfo(log: AuditLogRecord): {
  hasToken: boolean
  displayName: string
  tokenId: number | null
} {
  const rawName = typeof log.api_token_name === 'string' ? log.api_token_name.trim() : ''
  const tokenId = typeof log.api_token_id === 'number' ? log.api_token_id : null
  let displayName = ''
  if (rawName !== '') {
    displayName = rawName
  } else if (tokenId !== null) {
    displayName = `Token #${tokenId}`
  }
  return {
    hasToken: displayName !== '' || tokenId !== null,
    displayName,
    tokenId,
  }
}

export function resourceLabelForLog(log: AuditLogRecord): string {
  const details = safeParseDetails(log.details)
  let label = ''
  const actionType = log.action_type ?? ''
  const actionName = log.action ?? ''
  if (actionType === 'users' || actionName.startsWith('user.')) {
    label = (details.email as string) || ''
  } else if (actionType === 'tokens' || actionName.startsWith('api_token.')) {
    label = (details.name as string) || ''
  }
  if (!label) {
    label = (log.resource || '').trim() || '-'
  }
  return label
}

export function extractExecCommand(log: AuditLogRecord): string {
  const raw = (() => {
    try {
      const parsed = JSON.parse(log.details ?? '{}') as { command?: string }
      return (log.command ?? parsed.command ?? '').trim()
    } catch {
      return (log.command ?? '').trim()
    }
  })()
  if ((raw.startsWith("'") && raw.endsWith("'")) || (raw.startsWith('"') && raw.endsWith('"'))) {
    return raw.slice(1, -1)
  }
  return raw
}

export function formatStatusLabel(log: AuditLogRecord): string {
  if (log.status === 'denied') {
    return 'denied (RBAC)'
  }
  if ((log.status === 'failed' || log.status === 'error') && typeof log.http_status === 'number') {
    return `${log.status} (${log.http_status})`
  }
  return log.status ?? '-'
}
