import type { FormView } from './form-sections'
import type { MFAVerificationFormProps } from './types'

type MFAMode = NonNullable<MFAVerificationFormProps['mode']>

const WEBAUTHN_DESCRIPTIONS: Record<MFAMode, string> = {
  'step-up': 'Use your security key or biometric device to continue with this sensitive action.',
  cli: 'Use your security key or Touch ID to approve this CLI login request.',
  web: 'Click the button below to authenticate with your security key or biometric device.',
}

const TOTP_DESCRIPTIONS: Record<MFAMode, string> = {
  'step-up':
    'Enter the 6-digit verification code from your authenticator app to continue with this sensitive action.',
  cli: 'Enter the 6-digit code from your authenticator app to approve this CLI login request.',
  web: 'Enter the 6-digit code from your authenticator app to finish signing in.',
}

const BACKUP_CODE_DESCRIPTION =
  'Enter one of the backup codes you saved when you set up multi-factor authentication. Each code works once.'

/** Explains what the current MFA view asks the user to do. */
export function describeMFAView(view: FormView, mode: MFAMode): string {
  if (view === 'backup') {
    return BACKUP_CODE_DESCRIPTION
  }
  if (view === 'webauthn') {
    return WEBAUTHN_DESCRIPTIONS[mode]
  }
  return TOTP_DESCRIPTIONS[mode]
}
