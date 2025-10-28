import { useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { LoadingSpinner } from '@/components/loading-spinner'
import { MFAInput } from '@/components/mfa-input'
import { Button } from '@/components/ui/button'
import { getMFAStatus, startWebAuthnAssertion } from '@/lib/api'
import { getErrorMessage } from '@/lib/error-utils'
import {
  getCredential,
  prepareRequestOptions,
  serializeAssertionCredential,
} from '@/lib/webauthn-utils'

const DIGITS_ONLY_REGEX = /^\d+$/
const SIX_DIGITS_REGEX = /^\d{6}$/

type MFAMethod = 'totp' | 'webauthn'
type MFAMode = 'step-up' | 'cli' | 'web'

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

type MFAVerificationFormProps = {
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
   * Called when MFA status is loaded. Use to check enrollment state.
   */
  onMFAStatusLoaded?: (mfaStatus: Awaited<ReturnType<typeof getMFAStatus>>) => void

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
   * Mode determines the description text shown to the user
   * @default 'web'
   */
  mode?: MFAMode

  /**
   * Optional cancel button to render at the bottom right (e.g., for dialog)
   */
  renderCancelButton?: () => ReactNode
}

/**
 * Reusable MFA verification form component that wraps the common
 * TOTP and WebAuthn flows. It exposes callbacks for verification,
 * success, and error handling while providing the standard UI for
 * method selection, trust device prompts, and form layout.
 */
export function MFAVerificationForm({
  onVerify,
  onSuccess,
  onError,
  onMFAStatusLoaded,
  autoFocus = true,
  showTrustDevice = true,
  trustDeviceDefault = true,
  allowMethodSwitch = true,
  preferredMethod = 'auto',
  autoTriggerWebAuthn = false,
  mode = 'web',
  renderCancelButton,
}: MFAVerificationFormProps) {
  const [code, setCode] = useState('')
  const [trustDevice, setTrustDevice] = useState(trustDeviceDefault)
  const [error, setError] = useState<string | null>(null)
  const [useWebAuthn, setUseWebAuthn] = useState(false)
  const [isVerifying, setIsVerifying] = useState(false)
  const [inputVersion, setInputVersion] = useState(0)
  const lastSubmittedCodeRef = useRef<string | null>(null)
  const pendingCodeRef = useRef<string | null>(null)

  useEffect(() => {
    ;(globalThis as { __mfaCodeValue?: string }).__mfaCodeValue = code
  }, [code])

  // Fetch MFA status to determine available methods
  const { data: mfaStatus } = useQuery({
    queryKey: ['mfa-status'],
    queryFn: getMFAStatus,
    retry: false,
    staleTime: 30_000,
  })

  // Notify parent when MFA status is loaded
  useEffect(() => {
    if (mfaStatus && onMFAStatusLoaded) {
      onMFAStatusLoaded(mfaStatus)
    }
  }, [mfaStatus, onMFAStatusLoaded])

  const hasWebAuthn = (mfaStatus?.methods?.filter((m) => m.type === 'webauthn').length ?? 0) > 0
  const hasTOTP = (mfaStatus?.methods?.filter((m) => m.type === 'totp').length ?? 0) > 0

  // biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Method resolution checks multiple preference branches
  const resolvedInitialMethod = useMemo<MFAMethod | null>(() => {
    if (!mfaStatus) {
      return null
    }

    if (preferredMethod === 'totp' && hasTOTP) {
      return 'totp'
    }

    if (preferredMethod === 'webauthn' && hasWebAuthn) {
      return 'webauthn'
    }

    if (preferredMethod === 'auto') {
      if (mfaStatus.preferred_method === 'webauthn' && hasWebAuthn) {
        return 'webauthn'
      }
      if (mfaStatus.preferred_method === 'totp' && hasTOTP) {
        return 'totp'
      }
    }

    if (hasWebAuthn && !hasTOTP) {
      return 'webauthn'
    }

    return 'totp'
  }, [hasTOTP, hasWebAuthn, mfaStatus, preferredMethod])

  useEffect(() => {
    if (!(mfaStatus && resolvedInitialMethod)) {
      return
    }
    setUseWebAuthn(resolvedInitialMethod === 'webauthn')
  }, [mfaStatus, resolvedInitialMethod])

  const handleVerifyTotp = useCallback(
    async (codeOverride?: string) => {
      const codeToVerify = codeOverride ?? code
      if (!codeToVerify || codeToVerify.trim().length < 6) {
        setError('Enter a valid verification code')
        return
      }

      setError(null)
      setIsVerifying(true)
      ;(globalThis as { __verifyCalls?: number }).__verifyCalls =
        ((globalThis as { __verifyCalls?: number }).__verifyCalls ?? 0) + 1

      try {
        ;(globalThis as { __lastVerifyCode?: string }).__lastVerifyCode = codeToVerify.trim()
        lastSubmittedCodeRef.current = codeToVerify.trim()
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
        setCode('')
        pendingCodeRef.current = null
        lastSubmittedCodeRef.current = null
        setInputVersion((version) => version + 1)
        onError?.(err)
      } finally {
        setIsVerifying(false)
      }
    },
    [code, trustDevice, onVerify, onSuccess, onError]
  )

  const trySubmitCode = useCallback(() => {
    const pending = pendingCodeRef.current
    if (useWebAuthn) {
      return
    }
    if (isVerifying) {
      return
    }

    if (!pending || pending.length !== 6 || !DIGITS_ONLY_REGEX.test(pending)) {
      return
    }
    if (lastSubmittedCodeRef.current === pending) {
      return
    }

    pendingCodeRef.current = null
    handleVerifyTotp(pending).catch(() => {
      /* errors handled in handleVerifyTotp */
    })
  }, [handleVerifyTotp, isVerifying, useWebAuthn])

  useEffect(() => {
    if (!isVerifying) {
      trySubmitCode()
    }
  }, [isVerifying, trySubmitCode])

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
  }, [
    autoTriggerWebAuthn,
    error,
    handleVerifyWebAuthn,
    hasWebAuthn,
    isVerifying,
    mfaStatus,
    useWebAuthn,
  ])

  // Generate dynamic description based on mode and method
  const getDescription = () => {
    if (useWebAuthn) {
      if (mode === 'step-up') {
        return 'Use your security key or biometric device to continue with this sensitive action.'
      }
      if (mode === 'cli') {
        return 'Use your security key or Touch ID to approve this CLI login request.'
      }
      return 'Click the button below to authenticate with your security key or biometric device.'
    }

    // TOTP descriptions
    if (mode === 'step-up') {
      return 'Enter the 6-digit verification code from your authenticator app to continue with this sensitive action.'
    }
    if (mode === 'cli') {
      return 'Enter the 6-digit code from your authenticator app to approve this CLI login request.'
    }
    return 'Enter the 6-digit code from your authenticator app to finish signing in.'
  }

  return (
    <div className="space-y-6">
      <p className="text-center text-muted-foreground text-sm">{getDescription()}</p>
      {error && (
        <div className="rounded-md border border-destructive bg-destructive/10 p-3 text-center text-destructive text-sm">
          {error}
        </div>
      )}
      {useWebAuthn ? (
        <div className="space-y-6">
          <div className="space-y-6">
            <div className="py-[10px]">
              <Button
                className="w-full"
                disabled={isVerifying}
                onClick={() => {
                  handleVerifyWebAuthn().catch(() => {
                    /* errors handled by onError */
                  })
                }}
              >
                {isVerifying ? (
                  <LoadingSpinner className="size-4" variant="white" />
                ) : (
                  'Authenticate with Security Key'
                )}
              </Button>
            </div>
            {showTrustDevice && (
              <div className="flex justify-center">
                <label className="flex items-center gap-2 text-sm">
                  <input
                    checked={trustDevice}
                    onChange={(event) => setTrustDevice(event.target.checked)}
                    type="checkbox"
                  />
                  Trust this device for 30 days
                </label>
              </div>
            )}
          </div>
          {allowMethodSwitch && hasTOTP && hasWebAuthn && (
            <div className="space-y-6">
              <div className="border-t" />
              <Button
                className="w-full"
                onClick={() => setUseWebAuthn(false)}
                type="button"
                variant="outline"
              >
                Use authenticator app instead
              </Button>
            </div>
          )}
          {renderCancelButton && <div className="flex justify-end">{renderCancelButton()}</div>}
        </div>
      ) : (
        <div className="space-y-6">
          <div className="space-y-6">
            <div className="flex flex-col items-center">
              <MFAInput
                autoFocus={autoFocus}
                disabled={isVerifying}
                key={inputVersion}
                maxLength={6}
                onChange={(event) => {
                  if (isVerifying) {
                    return
                  }
                  const normalized = event.target.value.trim()
                  ;(globalThis as { __lastOnChange?: string }).__lastOnChange = normalized
                  setError(null)
                  setCode(normalized)
                  if (normalized.length === 6 && DIGITS_ONLY_REGEX.test(normalized)) {
                    pendingCodeRef.current = normalized
                    trySubmitCode()
                  } else {
                    pendingCodeRef.current = null
                  }
                }}
                onComplete={(completedCode) => {
                  if (isVerifying) {
                    return
                  }
                  setCode(completedCode)
                  if (SIX_DIGITS_REGEX.test(completedCode)) {
                    pendingCodeRef.current = completedCode
                    trySubmitCode()
                  }
                }}
                value={code}
              />
            </div>
            {showTrustDevice && (
              <div className="flex justify-center">
                <label className="flex items-center gap-2 text-sm">
                  <input
                    checked={trustDevice}
                    onChange={(event) => setTrustDevice(event.target.checked)}
                    type="checkbox"
                  />
                  Trust this device for 30 days
                </label>
              </div>
            )}
          </div>
          {allowMethodSwitch && hasTOTP && hasWebAuthn && (
            <div className="space-y-6">
              <div className="border-t" />
              <Button
                className="w-full"
                disabled={isVerifying}
                onClick={() => {
                  handleVerifyWebAuthn().catch(() => {
                    /* errors handled by onError */
                  })
                }}
                type="button"
                variant="outline"
              >
                {isVerifying ? <LoadingSpinner className="size-4" /> : 'Use security key instead'}
              </Button>
            </div>
          )}
          {renderCancelButton && <div className="flex justify-end">{renderCancelButton()}</div>}
        </div>
      )}
    </div>
  )
}
