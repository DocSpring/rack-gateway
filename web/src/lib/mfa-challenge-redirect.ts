import { WebRoute } from './routes'

const ROOT_PATHS = new Set(['', '/', WebRoute(), WebRoute('/')])

/**
 * Builds the MFA challenge URL for a session that still owes its login MFA, so the challenge
 * returns the user to the page they asked for.
 */
export function buildMfaChallengeUrl(pathname: string, search = ''): string {
  const challenge = WebRoute('auth/mfa/challenge')
  if (ROOT_PATHS.has(pathname)) {
    return challenge
  }
  const params = new URLSearchParams({ redirect: `${pathname}${search}` })
  return `${challenge}?${params.toString()}`
}

/** Sends a session that still owes its login MFA to the challenge page. */
export function redirectToMfaChallenge(): void {
  if (typeof window === 'undefined') {
    return
  }
  const { pathname, search } = window.location
  window.location.assign(buildMfaChallengeUrl(pathname, search))
}
