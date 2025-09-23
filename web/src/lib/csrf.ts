const META_NAME = 'cgw-csrf-token'
const META_SELECTOR = `meta[name="${META_NAME}"]`
const PLACEHOLDER = 'CGW_CSRF_TOKEN'
const hasDOM = typeof document !== 'undefined'

let csrfCache: string | null = null

function readTokenFromMeta(): string | null {
  if (!hasDOM) {
    return null
  }
  const meta = document.querySelector<HTMLMetaElement>(META_SELECTOR)
  const value = meta?.content?.trim()
  if (!value || value === PLACEHOLDER) {
    return null
  }
  return value
}

export function setCsrfToken(token: string | null): void {
  const value = token?.trim() ?? ''
  if (value === '' || value === PLACEHOLDER) {
    csrfCache = null
  } else {
    csrfCache = value
  }
  if (!hasDOM) {
    return
  }
  const meta = document.querySelector<HTMLMetaElement>(META_SELECTOR)
  if (meta) {
    meta.content = value
    return
  }
  if (value) {
    const element = document.createElement('meta')
    element.name = META_NAME
    element.content = value
    document.head.appendChild(element)
  }
}

export function getCsrfToken(): string | null {
  if (csrfCache) {
    return csrfCache
  }
  const metaToken = readTokenFromMeta()
  if (metaToken) {
    csrfCache = metaToken
  }
  return csrfCache
}

export async function ensureCsrfToken(): Promise<string | null> {
  const existing = getCsrfToken()
  if (existing) {
    return existing
  }
  try {
    const response = await fetch('/.gateway/api/auth/web/csrf', { credentials: 'include' })
    const payload = await response.json().catch(() => null)
    const token = typeof payload?.token === 'string' ? payload.token.trim() : ''
    if (token) {
      setCsrfToken(token)
      return token
    }
  } catch (_error) {
    // ignore fetch errors; caller will retry later
  }
  return getCsrfToken()
}

const initialToken = readTokenFromMeta()
if (initialToken) {
  csrfCache = initialToken
}
