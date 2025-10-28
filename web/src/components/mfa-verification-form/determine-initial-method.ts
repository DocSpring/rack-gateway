import type { getMFAStatus } from '@/lib/api'
import type { MFAMethod } from './types'

type DetermineMethodArgs = {
  mfaStatus: Awaited<ReturnType<typeof getMFAStatus>> | undefined
  preferredMethod: MFAMethod | 'auto'
  hasTOTP: boolean
  hasWebAuthn: boolean
}

export function determineInitialMFAMethod({
  mfaStatus,
  preferredMethod,
  hasTOTP,
  hasWebAuthn,
}: DetermineMethodArgs): MFAMethod | null {
  if (!mfaStatus) {
    return null
  }

  const preferenceOrder: Array<MFAMethod | 'auto'> = [preferredMethod]

  if (mfaStatus.preferred_method === 'webauthn' || mfaStatus.preferred_method === 'totp') {
    preferenceOrder.push(mfaStatus.preferred_method)
  }

  for (const preference of preferenceOrder) {
    if (preference === 'webauthn' && hasWebAuthn) {
      return 'webauthn'
    }
    if (preference === 'totp' && hasTOTP) {
      return 'totp'
    }
  }

  if (hasWebAuthn && !hasTOTP) {
    return 'webauthn'
  }

  if (hasTOTP) {
    return 'totp'
  }

  if (hasWebAuthn) {
    return 'webauthn'
  }

  return null
}
