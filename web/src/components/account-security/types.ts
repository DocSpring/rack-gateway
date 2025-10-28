import type { StartTOTPEnrollmentResponse, StartWebAuthnEnrollmentResponse } from '@/lib/api'

export type MFAMethodType = 'totp' | 'webauthn'

export type EnrollmentState = (StartTOTPEnrollmentResponse | StartWebAuthnEnrollmentResponse) & {
  qrDataUrl?: string | null
  enrollmentType?: MFAMethodType
}

const DEFAULT_TOTP_LABEL = 'Authenticator App'
export const DEFAULT_WEBAUTHN_LABEL = 'Security Key'

const DEFAULT_LABELS: Record<MFAMethodType, string> = {
  totp: DEFAULT_TOTP_LABEL,
  webauthn: DEFAULT_WEBAUTHN_LABEL,
}

export const MFA_METHOD_TYPE_LABELS: Record<string, string> = {
  totp: 'TOTP',
  webauthn: 'WebAuthn',
}

export const getDefaultLabelForType = (type?: MFAMethodType | string | null): string => {
  const normalized: MFAMethodType | null =
    type === 'totp' || type === 'webauthn' ? (type as MFAMethodType) : null
  if (!normalized) {
    return DEFAULT_TOTP_LABEL
  }
  return DEFAULT_LABELS[normalized]
}
