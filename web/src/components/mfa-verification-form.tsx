import { useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useCallback, useEffect, useState } from 'react'
import { MFAInput } from '@/components/mfa-input'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { getMFAStatus, startWebAuthnAssertion } from '@/lib/api'
import { getErrorMessage } from '@/lib/error-utils'
import {
  getCredential,
  prepareRequestOptions,
  serializeAssertionCredential,
} from '@/lib/webauthn-utils'

type MFAMethod = 'totp' | 'webauthn'

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

export type MFAVerificationFormProps = {
  /**
   * Called when verification needs to happen. Should perform the actual API call.
   * For TOTP: receives code and trust_device
   * For WebAuthn: receives session_data, assertion_response, and trust_device
   */
  onVerify: (params: VerificationParams) => Promise<void>

  /**
   * Called after successful verification. Use for navigation, state updates, etc.
   */
  onSuccess?: () => void | Promise<void>

  /**
   * Called when verification fails. Use for error display or logging.
   */
  onError?: (error: unknown) => void

  /**
   * Auto-focus the TOTP input when rendered
   * @default true
   */
  autoFocus?: boolean

  /**
   * Show the "Trust this device" checkbox
   * @default true
   */
  showTrustDevice?: boolean

  /**
   * Default value for trust device checkbox
   * @default true
   */
  trustDeviceDefault?: boolean

  /**
   * Allow switching between TOTP and WebAuthn methods
   * @default true
   */
  allowMethodSwitch?: boolean

  /**
   * Preferred method to show initially. 'auto' detects from MFA status.
   * @default 'auto'
   */
  preferredMethod?: MFAMethod | 'auto'

  /**
   * Auto-trigger WebAuthn verification when it's selected
   * @default false
   */
  autoTriggerWebAuthn?: boolean

  /**
   * Render function that receives state and components.
   * Allows full control over layout while reusing verification logic.
   */
  children: (props: MFAVerificationFormRenderProps) => ReactNode
}

export type MFAVerificationFormRenderProps = {
  // State
  isVerifying: boolean
  error: string | null
  code: string
  trustDevice: boolean
  useWebAuthn: boolean
  hasWebAuthn: boolean
  hasTOTP: boolean

  // Setters
  setCode: (code: string) => void
  setTrustDevice: (trust: boolean) => void
  setUseWebAuthn: (use: boolean) => void
  setError: (error: string | null) => void

  // Handlers
  handleVerifyTotp: (codeOverride?: string) => Promise<void>
  handleVerifyWebAuthn: () => Promise<void>

  // Pre-built components (optional, for convenience)
  TOTPInput: ReactNode
  TrustDeviceCheckbox: ReactNode
  MethodSwitchButtons: ReactNode
}

/**
 * Reusable MFA verification form component using render props pattern.
 *
 * Handles all the common logic for MFA verification:
 * - Fetching MFA status
 * - Managing TOTP/WebAuthn method selection
 * - TOTP input with auto-submit
 * - WebAuthn assertion flow
 * - Trust device checkbox
 * - Method switching UI
 * - Error handling
 *
 * The render props pattern allows consumers to control the layout completely
 * while reusing all verification logic.
 *
 * @example
 * ```tsx
 * <MFAVerificationForm
 *   onVerify={async (params) => {
 *     if (params.method === 'totp') {
 *       await verifyMFA({ code: params.code, trust_device: params.trust_device })
 *     } else {
 *       await verifyWebAuthnAssertion({ ...params })
 *     }
 *   }}
 *   onSuccess={() => navigate('/dashboard')}
 * >
 *   {({ TOTPInput, TrustDeviceCheckbox, useWebAuthn }) => (
 *     <Card>
 *       <CardContent>
 *         {useWebAuthn ? <WebAuthnButton /> : TOTPInput}
 *         {TrustDeviceCheckbox}
 *       </CardContent>
 *     </Card>
 *   )}
 * </MFAVerificationForm>
 * ```
 */
export function MFAVerificationForm({
  onVerify,
  onSuccess,
  onError,
  autoFocus = true,
  showTrustDevice = true,
  trustDeviceDefault = true,
  allowMethodSwitch = true,
  preferredMethod = 'auto',
  autoTriggerWebAuthn = false,
  children,
}: MFAVerificationFormProps) {
  const [code, setCode] = useState('')
  const [trustDevice, setTrustDevice] = useState(trustDeviceDefault)
  const [error, setError] = useState<string | null>(null)
  const [useWebAuthn, setUseWebAuthn] = useState(false)
  const [isVerifying, setIsVerifying] = useState(false)

  // Fetch MFA status to determine available methods
  const { data: mfaStatus } = useQuery({
    queryKey: ['mfa-status'],
    queryFn: getMFAStatus,
    retry: false,
    staleTime: 30_000,
  })

  const hasWebAuthn = (mfaStatus?.methods?.filter((m) => m.type === 'webauthn').length ?? 0) > 0
  const hasTOTP = (mfaStatus?.methods?.filter((m) => m.type === 'totp').length ?? 0) > 0

  // Set initial method based on preferred method or available methods
  // biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Method selection logic requires checking multiple conditions
  useEffect(() => {
    if (!mfaStatus) return

    if (preferredMethod === 'totp' && hasTOTP) {
      setUseWebAuthn(false)
      return
    }

    if (preferredMethod === 'webauthn' && hasWebAuthn) {
      setUseWebAuthn(true)
      return
    }

    // Auto-detect: Use server's preferred method if set
    if (preferredMethod === 'auto') {
      if (mfaStatus.preferred_method === 'webauthn' && hasWebAuthn) {
        setUseWebAuthn(true)
        return
      }
      if (mfaStatus.preferred_method === 'totp' && hasTOTP) {
        setUseWebAuthn(false)
        return
      }
    }

    // Fallback: If only WebAuthn available, use it
    if (hasWebAuthn && !hasTOTP) {
      setUseWebAuthn(true)
      return
    }

    // Default to TOTP
    setUseWebAuthn(false)
  }, [mfaStatus, hasWebAuthn, hasTOTP, preferredMethod])

  const handleVerifyTotp = useCallback(
    async (codeOverride?: string) => {
      const codeToVerify = codeOverride ?? code
      if (!codeToVerify || codeToVerify.trim().length < 6) {
        setError('Enter a valid verification code')
        return
      }

      setError(null)
      setIsVerifying(true)

      try {
        await onVerify({
          method: 'totp',
          code: codeToVerify.trim(),
          trust_device: trustDevice,
        })

        // Success
        await onSuccess?.()
      } catch (err) {
        const message = getErrorMessage(err, 'Verification failed')
        setError(message)
        onError?.(err)
      } finally {
        setIsVerifying(false)
      }
    },
    [code, trustDevice, onVerify, onSuccess, onError]
  )

  const handleVerifyWebAuthn = useCallback(async () => {
    setError(null)
    setIsVerifying(true)

    try {
      // Start WebAuthn assertion flow
      const assertionStart = await startWebAuthnAssertion()

      if (!assertionStart.options) {
        throw new Error('No assertion options received from server')
      }

      // Convert server options to browser-compatible format
      const credentialRequestOptions = prepareRequestOptions(assertionStart.options)

      // Call browser WebAuthn API
      const credential = await getCredential({
        publicKey: credentialRequestOptions,
      })

      if (!credential) {
        throw new Error('No credential received from authenticator')
      }

      // Serialize for backend
      const assertionResponse = serializeAssertionCredential(credential as PublicKeyCredential)

      // Verify with backend
      await onVerify({
        method: 'webauthn',
        session_data: assertionStart.session_data ?? '',
        assertion_response: JSON.stringify(assertionResponse),
        trust_device: trustDevice,
      })

      // Success
      await onSuccess?.()
    } catch (err) {
      const message = getErrorMessage(err, 'Verification failed')
      setError(message)
      onError?.(err)
    } finally {
      setIsVerifying(false)
    }
  }, [trustDevice, onVerify, onSuccess, onError])

  // Auto-trigger WebAuthn verification when it's the user's preferred method
  // Only triggers when server says preferred_method is webauthn, not when user manually switches
  // biome-ignore lint/correctness/useExhaustiveDependencies: only trigger on useWebAuthn change
  useEffect(() => {
    // Don't auto-trigger until MFA status is loaded
    if (!mfaStatus) return
    // Don't auto-trigger if user doesn't have WebAuthn enrolled
    if (!hasWebAuthn) return

    if (
      autoTriggerWebAuthn &&
      useWebAuthn &&
      !isVerifying &&
      !error &&
      mfaStatus.preferred_method === 'webauthn'
    ) {
      handleVerifyWebAuthn().catch(() => {
        /* errors handled in handleVerifyWebAuthn */
      })
    }
  }, [useWebAuthn, autoTriggerWebAuthn, hasWebAuthn, mfaStatus])

  // Pre-built components for convenience
  const TOTPInput = (
    <div className="space-y-2">
      <Label htmlFor="mfa-code">Verification code</Label>
      <MFAInput
        autoFocus={autoFocus}
        id="mfa-code"
        maxLength={6}
        onChange={(event) => {
          setError(null)
          setCode(event.target.value.trim())
        }}
        onComplete={(completedCode) => {
          setCode(completedCode)
          handleVerifyTotp(completedCode).catch(() => {
            /* errors handled in handleVerifyTotp */
          })
        }}
        placeholder="123456"
        required
        value={code}
      />
    </div>
  )

  const TrustDeviceCheckbox = showTrustDevice ? (
    <label className="flex items-center gap-2 text-sm">
      <input
        checked={trustDevice}
        onChange={(event) => setTrustDevice(event.target.checked)}
        type="checkbox"
      />
      Trust this device for 30 days
    </label>
  ) : null

  const MethodSwitchButtons =
    allowMethodSwitch && hasTOTP && hasWebAuthn ? (
      <div className="border-t pt-4">
        <Button
          className="w-full"
          onClick={() => setUseWebAuthn(!useWebAuthn)}
          type="button"
          variant="outline"
        >
          {useWebAuthn ? 'Use authenticator app instead' : 'Use security key instead'}
        </Button>
      </div>
    ) : null

  return (
    <>
      {children({
        // State
        isVerifying,
        error,
        code,
        trustDevice,
        useWebAuthn,
        hasWebAuthn,
        hasTOTP,

        // Setters
        setCode,
        setTrustDevice,
        setUseWebAuthn,
        setError,

        // Handlers
        handleVerifyTotp,
        handleVerifyWebAuthn,

        // Components
        TOTPInput,
        TrustDeviceCheckbox,
        MethodSwitchButtons,
      })}
    </>
  )
}
