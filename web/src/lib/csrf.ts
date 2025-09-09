let csrfCache: string | null = null

export function setCsrfToken(token: string) {
  csrfCache = token
}

export function getCsrfToken(): string | null {
  return csrfCache
}

export async function ensureCsrfToken(): Promise<string | null> {
  if (csrfCache) {
    return csrfCache
  }
  try {
    const r = await fetch('/.gateway/api/auth/web/csrf', { credentials: 'include' })
    const j = await r.json().catch(() => null)
    if (j?.token) {
      setCsrfToken(j.token)
      return j.token
    }
  } catch (_e) {
    // ignore
  }
  return null
}
// Best-effort eager fetch on module load
;(function init() {
  ensureCsrfToken().catch(() => {
    /* ignore */
  })
})()
