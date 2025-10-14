import { APIRoute } from '@/lib/routes'

import type { HttpRouteMfaRequirement } from './generated/mfa-requirements'
import { HTTP_ROUTE_MFA_REQUIREMENTS } from './generated/mfa-requirements'

type RequirementWithRegex = HttpRouteMfaRequirement & {
  regex?: RegExp
}

const exactIndex = new Map<string, RequirementWithRegex>()
const methodBuckets = new Map<string, RequirementWithRegex[]>()

for (const requirement of HTTP_ROUTE_MFA_REQUIREMENTS) {
  const method = requirement.method.toUpperCase()
  const key = `${method} ${requirement.pattern}`

  const record: RequirementWithRegex = {
    ...requirement,
  }

  exactIndex.set(key, record)

  const list = methodBuckets.get(method)
  if (list) {
    list.push(record)
  } else {
    methodBuckets.set(method, [record])
  }
}

const PARAM_SEGMENT = /^:([A-Za-z0-9_]+)$/
const QUERY_SPLIT_REGEX = /[?#]/
const PROTOCOL_PREFIX_REGEX = /^https?:\/\/[^/]+/i
const TRAILING_SLASH_REGEX = /\/+$/

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

function patternToRegex(pattern: string): RegExp {
  const segments = pattern.split('/').map((segment) => segment.trim())
  const transformed = segments
    .map((segment) => {
      if (segment === '') {
        return ''
      }
      if (segment === '*path') {
        return '.*'
      }
      if (PARAM_SEGMENT.test(segment)) {
        return '[^/]+'
      }
      return escapeRegExp(segment)
    })
    .join('/')
  return new RegExp(`^${transformed}$`)
}

function getRegex(requirement: RequirementWithRegex): RegExp {
  if (!requirement.regex) {
    requirement.regex = patternToRegex(requirement.pattern)
  }
  return requirement.regex
}

function normalizePath(input: string, baseUrl?: string): string {
  const [cleanInput] = input.split(QUERY_SPLIT_REGEX, 1)
  let path = cleanInput.startsWith('/') ? cleanInput : `/${cleanInput}`

  if (baseUrl) {
    const [cleanBase] = baseUrl.split(QUERY_SPLIT_REGEX, 1)
    let basePath = cleanBase.replace(PROTOCOL_PREFIX_REGEX, '')
    if (basePath && !basePath.startsWith('/')) {
      basePath = `/${basePath}`
    }
    basePath = basePath.replace(TRAILING_SLASH_REGEX, '')

    if (basePath && basePath !== '/' && !path.startsWith(basePath)) {
      path = `${basePath}${path}`
    }
  }

  if (!path.startsWith('/api/')) {
    const route = APIRoute()
    const normalizedRoute = route.endsWith('/') && path.startsWith('/') ? route.slice(0, -1) : route
    if (!path.startsWith(normalizedRoute)) {
      path = `${normalizedRoute}${path}`
    }
  }

  return path.replace(/\/{2,}/g, '/')
}

function findRequirement(
  method: string,
  path: string,
  baseUrl?: string
): RequirementWithRegex | null {
  const normalizedMethod = method.toUpperCase()
  const normalizedPath = normalizePath(path, baseUrl)

  const direct = exactIndex.get(`${normalizedMethod} ${normalizedPath}`)
  if (direct) {
    return direct
  }

  const candidates = methodBuckets.get(normalizedMethod)
  if (!candidates) {
    return null
  }

  for (const candidate of candidates) {
    if (candidate.pattern === normalizedPath) {
      return candidate
    }
    if (!(candidate.pattern.includes(':') || candidate.pattern.includes('*'))) {
      continue
    }
    if (getRegex(candidate).test(normalizedPath)) {
      return candidate
    }
  }

  return null
}

export function shouldPreemptMfaRequest(
  method: string | undefined,
  url: string | undefined,
  baseUrl?: string
): boolean {
  if (!(method && url)) {
    return false
  }
  const requirement = findRequirement(method, url, baseUrl)
  if (!requirement) {
    return false
  }
  return requirement.mfaLevel === 'always'
}

export function getMfaRequirementForRequest(
  method: string | undefined,
  url: string | undefined,
  baseUrl?: string
): RequirementWithRegex | null {
  if (!(method && url)) {
    return null
  }
  return findRequirement(method, url, baseUrl)
}
