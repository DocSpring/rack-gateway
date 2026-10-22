import { isAxiosError } from 'axios'
import { verifyMFA, verifyWebAuthnAssertion } from '@/lib/api'

/** An answer to the MFA prompt: an authenticator app, YubiKey or backup code, or a security key assertion. */
export interface StepUpVerification {
  method: 'totp' | 'webauthn'
  code?: string
  trust_device: boolean
  session_data?: string
  assertion_response?: string
}

interface MfaHeaders {
  'X-MFA-TOTP'?: string
  'X-MFA-WebAuthn'?: string
}

let currentMFAHeaders: MfaHeaders = {}

export function getMFAHeaders(): MfaHeaders {
  return currentMFAHeaders
}

export function clearMFAHeaders(): void {
  currentMFAHeaders = {}
}

/** Sets the answer as the MFA proof sent with the next retried request. */
export function setMFAHeaders(params: StepUpVerification): void {
  clearMFAHeaders()
  if (params.method === 'totp') {
    currentMFAHeaders['X-MFA-TOTP'] = params.code ?? ''
    return
  }
  const payload = JSON.stringify({
    session_data: params.session_data,
    assertion_response: params.assertion_response,
  })
  currentMFAHeaders['X-MFA-WebAuthn'] = btoa(payload)
}

/** Has the gateway check the answer, which opens a step-up window when it's right. */
export async function verifyStepUp(params: StepUpVerification): Promise<void> {
  if (params.method === 'totp') {
    await verifyMFA({ code: params.code ?? '', trust_device: params.trust_device })
    return
  }
  await verifyWebAuthnAssertion({
    session_data: params.session_data ?? '',
    assertion_response: params.assertion_response ?? '',
    trust_device: params.trust_device,
  })
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
