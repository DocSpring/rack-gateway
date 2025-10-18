const META_NAME = 'rgw-csrf-token'
const META_SELECTOR = `meta[name="${META_NAME}"]`
const PLACEHOLDER = 'RGW_CSRF_TOKEN'
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

const initialToken = readTokenFromMeta()
if (initialToken) {
  csrfCache = initialToken
}
