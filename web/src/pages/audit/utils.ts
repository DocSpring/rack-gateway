import { format } from 'date-fns'

import { DEFAULT_PER_PAGE } from '@/lib/constants'

export const RESOURCE_TYPES = {
  all: 'All Resources',
  app: 'App',
  rack: 'Rack',
  env: 'Env',
  secret: 'Secret',
  process: 'Process',
  system: 'System',
  api_token: 'API Token',
  user: 'User',
  auth: 'Auth',
  admin: 'Admin',
} as const

export const ACTION_TYPES = {
  all: 'All Actions',
  convox: 'Convox API',
  auth: 'Authentication',
  users: 'User Management',
  tokens: 'Token Management',
} as const

export const STATUS_TYPES = {
  all: 'All Statuses',
  success: 'Success',
  failed: 'Failed',
  blocked: 'Blocked',
} as const

export const VALID_DATE_RANGES = new Set(['15m', '1h', '24h', '7d', '30d', 'all', 'custom'])
export const DEFAULT_DATE_RANGE = '7d'
const DATE_SPLIT_PATTERN = /[T\s]/
const TIME_PATTERN = /^[0-9]{2}:[0-9]{2}$/

type PaginationOptions = {
  page?: number
  limit?: number
  includeDefaultPagination: boolean
}

type RangeOptions = {
  range: string
  customStart?: string
  customEnd?: string
  includeDefaultRange: boolean
}

export function createAuditQueryParams(options: {
  actionType: string
  status: string
  resourceType: string
  range: string
  customStart?: string
  customEnd?: string
  search?: string
  userId?: string
  userEmail?: string
  page?: number
  limit?: number
  includeDefaultRange?: boolean
  includeDefaultPagination?: boolean
}): URLSearchParams {
  const params = new URLSearchParams()
  const {
    actionType,
    status,
    resourceType,
    range,
    customStart,
    customEnd,
    search,
    userId,
    userEmail,
    page,
    limit,
    includeDefaultRange = false,
    includeDefaultPagination = false,
  } = options

  appendSimpleFilters(params, [
    ['action_type', actionType, 'all'],
    ['status', status, 'all'],
    ['resource_type', resourceType, 'all'],
  ])

  appendRangeFilters(params, {
    range,
    customStart,
    customEnd,
    includeDefaultRange,
  })

  if (search) {
    params.set('search', search)
  }

  if (userEmail) {
    params.set('user', userEmail)
  }

  if (userId) {
    params.set('user_id', userId)
  }

  appendPagination(params, {
    page,
    limit,
    includeDefaultPagination,
  })

  return params
}

function appendSimpleFilters(
  params: URLSearchParams,
  entries: [string, string | undefined, string][]
) {
  for (const [key, value, skip] of entries) {
    if (value && value !== skip) {
      params.set(key, value)
    }
  }
}

function appendRangeFilters(params: URLSearchParams, options: RangeOptions) {
  const { range, customStart, customEnd, includeDefaultRange } = options
  if (!range) {
    return
  }

  if (includeDefaultRange || range !== DEFAULT_DATE_RANGE) {
    params.set('range', range)
  }

  if (range === 'custom') {
    if (customStart) {
      params.set('start', customStart)
    }
    if (customEnd) {
      params.set('end', customEnd)
    }
  }
}

function appendPagination(params: URLSearchParams, options: PaginationOptions) {
  const { page, limit, includeDefaultPagination } = options

  const shouldIncludePage =
    typeof page === 'number' && page > 0 && (includeDefaultPagination || page > 1)
  if (shouldIncludePage) {
    params.set('page', String(page))
  }

  const shouldIncludeLimit =
    typeof limit === 'number' &&
    limit > 0 &&
    (includeDefaultPagination || limit !== DEFAULT_PER_PAGE)
  if (shouldIncludeLimit) {
    params.set('limit', String(limit))
  }
}

export function toDateTimeLocalInput(value?: string | null): string {
  if (!value) {
    return ''
  }

  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value.length >= 16 ? value.slice(0, 16) : ''
  }

  const offsetMinutes = date.getTimezoneOffset()
  const local = new Date(date.getTime() - offsetMinutes * 60_000)
  return local.toISOString().slice(0, 16)
}

function parseLocalDateTime(value: string | undefined) {
  if (!value) {
    return null
  }

  const { datePart, timePart } = splitDateTime(value)
  if (!datePart) {
    return null
  }

  const [year, month, day] = datePart.split('-').map(Number)
  if ([year, month, day].some(Number.isNaN)) {
    return null
  }

  const safeTime = TIME_PATTERN.test(timePart) ? timePart : '00:00'
  const [hour, minute] = safeTime.split(':').map(Number)
  if ([hour, minute].some(Number.isNaN)) {
    return null
  }

  const local = new Date(year, month - 1, day, hour, minute, 0, 0)
  if (Number.isNaN(local.getTime())) {
    return null
  }

  return local
}

export function toISOStringParam(value?: string | null): string | undefined {
  if (!value) {
    return
  }

  const parsed = parseLocalDateTime(value)
  return parsed?.toISOString()
}

export function splitDateTime(value: string | undefined) {
  if (!value) {
    return { datePart: '', timePart: '' }
  }

  const trimmed = value.trim()
  const [rawDate = '', rawTime = ''] = trimmed.split('T')
  const datePart = rawDate.split(' ')[0] || ''
  const timeCandidate = rawTime || trimmed.split(' ')[1] || ''
  const truncatedTime = timeCandidate.slice(0, 5)

  return {
    datePart,
    timePart: TIME_PATTERN.test(truncatedTime) ? truncatedTime : '',
  }
}

export function combineDateTime(datePart: string, timePart: string) {
  const normalizedDate = datePart.trim().split(DATE_SPLIT_PATTERN)[0] || ''
  if (!normalizedDate) {
    return ''
  }
  const normalizedTime = timePart.trim().slice(0, 5)
  const safeTime = TIME_PATTERN.test(normalizedTime) ? normalizedTime : '00:00'
  return `${normalizedDate}T${safeTime}`
}

export function parseDateTime(value: string | undefined) {
  return parseLocalDateTime(value)
}

export function isValidDateTime(value: string | undefined) {
  return parseDateTime(value) !== null
}

export const ensureFilterValue = (
  value: string | null | undefined,
  options: Record<string, string>
) => (value && value in options ? value : 'all')

export function formatAuditExportFileName() {
  return `audit-logs-${format(new Date(), 'yyyy-MM-dd')}.csv`
}
