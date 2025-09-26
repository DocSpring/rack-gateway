const SPA_WEB_PREFIX = '/.gateway/web'

const coercePath = (input: string): string => {
  if (input.startsWith(SPA_WEB_PREFIX)) {
    const trimmed = input.slice(SPA_WEB_PREFIX.length)
    return trimmed === '' ? '/' : trimmed
  }
  if (!input.startsWith('/')) {
    return `/${input}`
  }
  return input
}

export function normalizeRedirectPath(value: string | null): string | null {
  if (!value) {
    return null
  }

  if (typeof window !== 'undefined') {
    try {
      const resolved = new URL(value, window.location.origin)
      const path = `${resolved.pathname}${resolved.search}${resolved.hash}`
      return coercePath(path)
    } catch (_error) {
      // fall back to plain string handling
    }
  }

  return coercePath(value)
}

export default normalizeRedirectPath
