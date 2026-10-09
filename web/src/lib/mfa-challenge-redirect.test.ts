import { describe, expect, it } from 'vitest'
import { buildMfaChallengeUrl } from './mfa-challenge-redirect'

describe('buildMfaChallengeUrl', () => {
  it('returns to the requested page after the challenge', () => {
    expect(buildMfaChallengeUrl('/app/users', '?page=2')).toBe(
      `/app/auth/mfa/challenge?redirect=${encodeURIComponent('/app/users?page=2')}`
    )
  })

  it('omits the redirect for the app root', () => {
    expect(buildMfaChallengeUrl('/app/')).toBe('/app/auth/mfa/challenge')
    expect(buildMfaChallengeUrl('/app')).toBe('/app/auth/mfa/challenge')
  })
})
