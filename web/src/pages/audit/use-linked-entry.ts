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

// dismissLinkedEntry drops ?entry from the URL once the detail dialog closes, so a reload doesn't open it again.
function dismissLinkedEntry(): void {
  if (typeof window === 'undefined') {
    return
  }
  const url = new URL(window.location.href)
  if (!url.searchParams.has('entry')) {
    return
  }
  url.searchParams.delete('entry')
  window.history.replaceState(null, '', `${url.pathname}${url.search}${url.hash}`)
}

// useLinkedAuditEntry loads the audit event the page's link names, for the detail dialog to open with. The ID is
// read once; the page's filter sync keeps ?entry in the URL until dismiss drops it.
export function useLinkedAuditEntry(enabled: boolean) {
  const [id] = useState(linkedEntryID)
  const query = useQuery<AuditLogEntry, Error>({
    queryKey: ['audit-log', id],
    queryFn: () => api.get<AuditLogEntry>(apiPath`/api/v1/audit-logs/${id ?? ''}`),
    enabled: enabled && id !== null,
    retry: false,
    staleTime: Number.POSITIVE_INFINITY,
  })
  return { entry: query.data ?? null, error: query.error, dismiss: dismissLinkedEntry }
}
