let csrfCache: string | null = null

export function setCsrfToken(token: string) {
  csrfCache = token
}

export function getCsrfToken(): string | null {
  return csrfCache
}
// Initialize on module load by fetching token once
;(function init() {
  try {
    fetch('/.gateway/api/csrf', { credentials: 'include' })
      .then((r) => r.json())
      .then((j) => {
        if (j?.token) {
          setCsrfToken(j.token)
        }
      })
      .catch(() => {
        /* ignore */
      })
  } catch (_e) {
    /* ignore */
  }
})()
