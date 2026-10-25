import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'

import { type AuditLogEntry, api } from '@/lib/api'
import { apiPath } from '@/lib/api-path'

const ENTRY_ID = /^[1-9]\d*$/

// linkedEntryID is the audit event ID the page's link names with ?entry=<id> (Slack messages link to the events
// they describe), or null.
function linkedEntryID(): string | null {
  if (typeof window === 'undefined') {
    return null
  }
  const id = new URLSearchParams(window.location.search).get('entry')
  return id && ENTRY_ID.test(id) ? id : null
}

// useLinkedAuditEntry loads the audit event the page's link names, for the detail dialog to open with. It's read
// once: the page's filter sync drops ?entry from the URL after the first render.
export function useLinkedAuditEntry(enabled: boolean) {
  const [id] = useState(linkedEntryID)
  return useQuery<AuditLogEntry, Error>({
    queryKey: ['audit-log', id],
    queryFn: () => api.get<AuditLogEntry>(apiPath`/api/v1/audit-logs/${id ?? ''}`),
    enabled: enabled && id !== null,
    retry: false,
    staleTime: Number.POSITIVE_INFINITY,
  })
}
