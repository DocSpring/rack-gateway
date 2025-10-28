import { isAxiosError } from 'axios'

type MfaHeaders = { 'X-MFA-TOTP'?: string; 'X-MFA-WebAuthn'?: string }

let currentMFAHeaders: MfaHeaders = {}

export function getMFAHeaders(): MfaHeaders {
  return currentMFAHeaders
}

export function clearMFAHeaders(): void {
  currentMFAHeaders = {}
}

export function setTotpHeader(code?: string): void {
  currentMFAHeaders['X-MFA-TOTP'] = code ?? ''
}

export function setWebAuthnHeader(params: {
  session_data?: string
  assertion_response?: string
}): void {
  const payload = JSON.stringify({
    session_data: params.session_data,
    assertion_response: params.assertion_response,
  })
  currentMFAHeaders['X-MFA-WebAuthn'] = btoa(payload)
}

export function isMFAError(error: unknown): boolean {
  if (!isAxiosError(error)) {
    return false
  }
  if (error.response?.status !== 401) {
    return false
  }
  const errorCode = (error.response?.data as { error?: string } | undefined)?.error
  const header = error.response?.headers?.['x-mfa-required']
  return (
    errorCode === 'mfa_step_up_required' ||
    errorCode === 'mfa_required' ||
    errorCode === 'mfa_verification_failed' ||
    header === 'step-up' ||
    header === 'always'
  )
}
