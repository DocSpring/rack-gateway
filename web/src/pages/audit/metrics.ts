import { useMemo } from 'react'

import type { AuditLogRecord } from '@/components/audit-logs-pane'

type AuditStats = {
  total: number
  success: number
  failed: number
  denied: number
  avgResponseTime: number
}

type AuditPagination = {
  totalPages: number
  currentPage: number
  firstRowIndex: number
  lastRowIndex: number
}

type AuditMetrics = {
  stats: AuditStats
  pagination: AuditPagination
}

export function useAuditMetrics({
  logs,
  perPage,
  totalCount,
  page,
}: {
  logs: readonly AuditLogRecord[]
  perPage: number
  totalCount: number
  page: number
}): AuditMetrics {
  return useMemo(
    () => calculateAuditMetrics({ logs, perPage, totalCount, page }),
    [logs, page, perPage, totalCount]
  )
}

function calculateAuditMetrics({
  logs,
  perPage,
  totalCount,
  page,
}: {
  logs: readonly AuditLogRecord[]
  perPage: number
  totalCount: number
  page: number
}): AuditMetrics {
  const normalizedPerPage = Math.max(1, perPage)
  const normalizedTotalCount = Math.max(0, totalCount)
  const totalPages = Math.max(1, Math.ceil(normalizedTotalCount / normalizedPerPage))
  const currentPage = Math.min(Math.max(page, 1), totalPages)
  const firstRowIndex = normalizedTotalCount === 0 ? 0 : (currentPage - 1) * normalizedPerPage + 1
  const lastRowIndex = normalizedTotalCount === 0 ? 0 : firstRowIndex + logs.length - 1

  const stats = calculateAuditStats(logs)

  return {
    stats,
    pagination: {
      totalPages,
      currentPage,
      firstRowIndex,
      lastRowIndex,
    },
  }
}

function calculateAuditStats(logs: readonly AuditLogRecord[]): AuditStats {
  let totalEvents = 0
  let successEvents = 0
  let failedEvents = 0
  let deniedEvents = 0
  let totalResponseTime = 0

  for (const log of logs) {
    const occurrences = Math.max(1, log.event_count ?? 1)
    totalEvents += occurrences

    if (log.status === 'success') {
      successEvents += occurrences
    } else if (log.status === 'failed') {
      failedEvents += occurrences
    } else if (log.status === 'denied' || log.status === 'blocked') {
      deniedEvents += occurrences
    }

    if (typeof log.response_time_ms === 'number' && log.response_time_ms > 0) {
      totalResponseTime += log.response_time_ms * occurrences
    }
  }

  const avgResponseTime = totalEvents > 0 ? Math.round(totalResponseTime / totalEvents) : 0

  return {
    total: totalEvents,
    success: successEvents,
    failed: failedEvents,
    denied: deniedEvents,
    avgResponseTime,
  }
}
