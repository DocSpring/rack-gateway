import { useLocation } from '@tanstack/react-router'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { DEFAULT_PER_PAGE } from '@/lib/constants'
import {
  ACTION_TYPES,
  createAuditQueryParams,
  DEFAULT_DATE_RANGE,
  ensureFilterValue,
  isValidDateTime,
  RESOURCE_TYPES,
  STATUS_TYPES,
  toDateTimeLocalInput,
  toISOStringParam,
  VALID_DATE_RANGES,
} from '@/pages/audit/utils'

export function useAuditSearchParams(userId?: string, userEmail?: string) {
  const location = useLocation()

  const initialSearchParams = useMemo(
    () => (typeof window !== 'undefined' ? new URLSearchParams(location.search) : undefined),
    [location.search]
  )
  const initialRangeParam = initialSearchParams?.get('range')
  const queryUserEmail = initialSearchParams?.get('user')?.trim() ?? ''
  const resolvedUserEmail = userEmail?.trim() || queryUserEmail

  const [searchTerm, setSearchTerm] = useState(
    () => initialSearchParams?.get('search') ?? resolvedUserEmail
  )
  const [debouncedSearch, setDebouncedSearch] = useState(searchTerm)
  const [actionTypeFilter, setActionTypeFilter] = useState(() =>
    ensureFilterValue(initialSearchParams?.get('action_type'), ACTION_TYPES)
  )
  const [statusFilter, setStatusFilter] = useState(() =>
    ensureFilterValue(initialSearchParams?.get('status'), STATUS_TYPES)
  )
  const [resourceTypeFilter, setResourceTypeFilter] = useState(() =>
    ensureFilterValue(initialSearchParams?.get('resource_type'), RESOURCE_TYPES)
  )
  const [dateRange, setDateRange] = useState(() => {
    const param = initialRangeParam
    if (param && (VALID_DATE_RANGES.has(param) || param === 'custom')) {
      return param
    }
    if (initialSearchParams?.has('start') || initialSearchParams?.has('end')) {
      return 'custom'
    }
    try {
      const stored = localStorage.getItem('audit_date_range')
      if (stored && (VALID_DATE_RANGES.has(stored) || stored === 'custom')) {
        return stored
      }
    } catch {
      /* ignore */
    }
    return DEFAULT_DATE_RANGE
  })
  const [perPage, setPerPage] = useState<number>(() => {
    const param = initialSearchParams?.get('limit')
    if (param) {
      const parsed = Number.parseInt(param, 10)
      if (Number.isFinite(parsed) && parsed > 0) {
        return parsed
      }
    }
    try {
      const storedValue = localStorage.getItem('audit_per_page')
      if (storedValue) {
        const parsed = Number.parseInt(storedValue, 10)
        if (Number.isFinite(parsed) && parsed > 0) {
          return parsed
        }
      }
      return DEFAULT_PER_PAGE
    } catch {
      return DEFAULT_PER_PAGE
    }
  })
  const [page, setPage] = useState(() => {
    const param = initialSearchParams?.get('page')
    if (param) {
      const parsed = Number.parseInt(param, 10)
      if (Number.isFinite(parsed) && parsed > 0) {
        return parsed
      }
    }
    return 1
  })
  const [queryUserId] = useState(() => initialSearchParams?.get('user_id') ?? '')
  const effectiveUserId = userId ?? (queryUserId || undefined)
  const searchInitializedRef = useRef(false)
  const [customStart, setCustomStart] = useState(() => {
    const fromQuery = initialSearchParams?.get('start')
    if (fromQuery) {
      return toDateTimeLocalInput(fromQuery)
    }
    if (
      typeof window !== 'undefined' &&
      (initialRangeParam === 'custom' || dateRange === 'custom')
    ) {
      try {
        const stored = localStorage.getItem('audit_custom_start')
        if (stored) {
          return stored
        }
      } catch {
        /* ignore */
      }
    }
    return ''
  })
  const [customEnd, setCustomEnd] = useState(() => {
    const fromQuery = initialSearchParams?.get('end')
    if (fromQuery) {
      return toDateTimeLocalInput(fromQuery)
    }
    if (
      typeof window !== 'undefined' &&
      (initialRangeParam === 'custom' || dateRange === 'custom')
    ) {
      try {
        const stored = localStorage.getItem('audit_custom_end')
        if (stored) {
          return stored
        }
      } catch {
        /* ignore */
      }
    }
    return ''
  })

  const customStartISO = useMemo(() => {
    if (dateRange !== 'custom') {
      return
    }
    return toISOStringParam(customStart)
  }, [customStart, dateRange])

  const customEndISO = useMemo(() => {
    if (dateRange !== 'custom') {
      return
    }
    return toISOStringParam(customEnd)
  }, [customEnd, dateRange])

  const handleCustomStartChange = useCallback((next: string) => {
    setCustomStart(next)
    setPage(1)
    if (!isValidDateTime(next)) {
      return
    }
    setCustomEnd((prev) => {
      if (!(prev && isValidDateTime(prev))) {
        return next
      }
      const prevTime = new Date(prev).getTime()
      const nextTime = new Date(next).getTime()
      return prevTime < nextTime ? next : prev
    })
  }, [])

  const handleCustomEndChange = useCallback((next: string) => {
    setCustomEnd(next)
    setPage(1)
    if (!isValidDateTime(next)) {
      return
    }
    setCustomStart((prev) => {
      if (!(prev && isValidDateTime(prev))) {
        return next
      }
      const prevTime = new Date(prev).getTime()
      const nextTime = new Date(next).getTime()
      return prevTime > nextTime ? next : prev
    })
  }, [])

  // Persist selected date range and per-page to localStorage
  useEffect(() => {
    try {
      localStorage.setItem('audit_date_range', dateRange)
    } catch (_e) {
      /* ignore */
    }
  }, [dateRange])
  useEffect(() => {
    try {
      localStorage.setItem('audit_per_page', String(perPage))
    } catch (_e) {
      /* ignore */
    }
  }, [perPage])

  useEffect(() => {
    if (dateRange !== 'custom') {
      return
    }
    try {
      if (customStart) {
        localStorage.setItem('audit_custom_start', customStart)
      } else {
        localStorage.removeItem('audit_custom_start')
      }
    } catch (_e) {
      /* ignore */
    }
  }, [customStart, dateRange])

  useEffect(() => {
    if (dateRange !== 'custom') {
      return
    }
    try {
      if (customEnd) {
        localStorage.setItem('audit_custom_end', customEnd)
      } else {
        localStorage.removeItem('audit_custom_end')
      }
    } catch (_e) {
      /* ignore */
    }
  }, [customEnd, dateRange])

  useEffect(() => {
    if (dateRange !== 'custom') {
      return
    }
    if (customStart || customEnd) {
      return
    }
    try {
      const storedStart = localStorage.getItem('audit_custom_start')
      const storedEnd = localStorage.getItem('audit_custom_end')
      if (storedStart) {
        setCustomStart(storedStart)
      }
      if (storedEnd) {
        setCustomEnd(storedEnd)
      }
    } catch (_e) {
      /* ignore */
    }
  }, [customStart, customEnd, dateRange])

  // Debounce search to prevent refetch on every keystroke
  useEffect(() => {
    if (searchInitializedRef.current) {
      setPage(1)
      const timer = setTimeout(() => setDebouncedSearch(searchTerm), 300)
      return () => clearTimeout(timer)
    }
    searchInitializedRef.current = true
    setDebouncedSearch(searchTerm)
  }, [searchTerm])

  const routingQuery = useMemo(
    () =>
      createAuditQueryParams({
        actionType: actionTypeFilter,
        status: statusFilter,
        resourceType: resourceTypeFilter,
        range: dateRange,
        customStart: customStartISO,
        customEnd: customEndISO,
        search: searchTerm,
        userId: effectiveUserId,
        userEmail: resolvedUserEmail,
        page,
        limit: perPage,
      }).toString(),
    [
      actionTypeFilter,
      customEndISO,
      customStartISO,
      dateRange,
      effectiveUserId,
      resolvedUserEmail,
      page,
      perPage,
      resourceTypeFilter,
      searchTerm,
      statusFilter,
    ]
  )

  useEffect(() => {
    if (typeof window === 'undefined') {
      return
    }

    const currentQuery = window.location.search.startsWith('?')
      ? window.location.search.slice(1)
      : window.location.search

    if (currentQuery === routingQuery) {
      return
    }

    const newUrl = `${location.pathname}${routingQuery ? `?${routingQuery}` : ''}${location.hash}`
    window.history.replaceState(null, '', newUrl)
  }, [location.hash, location.pathname, routingQuery])

  return {
    searchTerm,
    setSearchTerm,
    debouncedSearch,
    actionTypeFilter,
    setActionTypeFilter,
    statusFilter,
    setStatusFilter,
    resourceTypeFilter,
    setResourceTypeFilter,
    dateRange,
    setDateRange,
    perPage,
    setPerPage,
    page,
    setPage,
    customStart,
    setCustomStart,
    customEnd,
    setCustomEnd,
    customStartISO,
    customEndISO,
    effectiveUserId,
    resolvedUserEmail,
    searchInitializedRef,
    handleCustomStartChange,
    handleCustomEndChange,
  }
}
