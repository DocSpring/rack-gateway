import type { ReactNode } from 'react'
import type { getMFAStatus } from '@/lib/api'

export type MFAMethod = 'totp' | 'webauthn'
type MFAMode = 'step-up' | 'cli' | 'web'
type MFAStatus = Awaited<ReturnType<typeof getMFAStatus>>

type VerificationParams =
  | {
      method: 'totp'
      code: string
      trust_device: boolean
    }
  | {
      method: 'webauthn'
      trust_device: boolean
      session_data: string
      assertion_response: string
    }

export interface MFAVerificationFormProps {
  onVerify: (params: VerificationParams) => Promise<void>
  onSuccess?: () => void | Promise<void>
  onError?: (error: unknown) => void
  // showError false leaves showing a failed verification to the caller (onError); the form shows it by default
  showError?: boolean
  onMFAStatusLoaded?: (mfaStatus: MFAStatus) => void
  focusOnMount?: boolean
  showTrustDevice?: boolean
  trustDeviceDefault?: boolean
  allowMethodSwitch?: boolean
  preferredMethod?: MFAMethod | 'auto'
  autoTriggerWebAuthn?: boolean
  mode?: MFAMode
  renderCancelButton?: () => ReactNode
}
