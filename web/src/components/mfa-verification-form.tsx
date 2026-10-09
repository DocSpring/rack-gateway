import { useQuery } from '@tanstack/react-query'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { getMFAStatus, startWebAuthnAssertion } from '@/lib/api'
import { getErrorMessage } from '@/lib/error-utils'
import {
  getCredential,
  prepareRequestOptions,
  serializeAssertionCredential,
} from '@/lib/webauthn-utils'
import { describeMFAView } from './mfa-verification-form/description'
import { determineInitialMFAMethod } from './mfa-verification-form/determine-initial-method'
import {
  type FormView,
  type RenderProps,
  renderFormContent,
} from './mfa-verification-form/form-sections'
import type { MFAMethod, MFAVerificationFormProps } from './mfa-verification-form/types'

const DIGITS_ONLY_REGEX = /^\d+$/

function resolveFormView(
  isLoading: boolean,
  useBackupCode: boolean,
  useWebAuthn: boolean
): FormView {
  if (isLoading) {
    return 'loading'
  }
  if (useBackupCode) {
    return 'backup'
  }
  return useWebAuthn ? 'webauthn' : 'totp'
}

/**
 * Reusable MFA verification form component that wraps the common
 * TOTP, WebAuthn and backup-code flows. It exposes callbacks for verification,
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
  const [useBackupCode, setUseBackupCode] = useState(false)
  const [isVerifying, setIsVerifying] = useState(false)
  const [inputVersion, setInputVersion] = useState(0)
  const lastSubmittedCodeRef = useRef<string | null>(null)
  const pendingCodeRef = useRef<string | null>(null)
  const autoTriggeredRef = useRef(false)

  useEffect(() => {
    ;(globalThis as { __mfaCodeValue?: string }).__mfaCodeValue = code
  }, [code])

  // Fetch MFA status to determine available methods
  const { data: mfaStatus, isLoading: isMFAStatusLoading } = useQuery({
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
  const hasBackupCodes = (mfaStatus?.backup_codes?.unused ?? 0) > 0

  const resolvedInitialMethod = useMemo<MFAMethod | null>(
    () =>
      determineInitialMFAMethod({
        mfaStatus,
        preferredMethod,
        hasTOTP,
        hasWebAuthn,
      }),
    [hasTOTP, hasWebAuthn, mfaStatus, preferredMethod]
  )

  useEffect(() => {
    if (!(mfaStatus && resolvedInitialMethod)) {
      return
    }
    setUseWebAuthn(resolvedInitialMethod === 'webauthn')
  }, [mfaStatus, resolvedInitialMethod])

  // Submits a TOTP or backup code. Backup codes never trust the device: they are for recovery.
  const verifyCode = useCallback(
    async (codeToVerify: string, trust: boolean) => {
      const trimmed = codeToVerify.trim()
      if (trimmed.length < 6) {
        setError('Enter a valid verification code')
        return
      }

      setError(null)
      setIsVerifying(true)
      ;(globalThis as { __verifyCalls?: number }).__verifyCalls =
        ((globalThis as { __verifyCalls?: number }).__verifyCalls ?? 0) + 1

      try {
        ;(globalThis as { __lastVerifyCode?: string }).__lastVerifyCode = trimmed
        lastSubmittedCodeRef.current = trimmed
        await onVerify({ method: 'totp', code: trimmed, trust_device: trust })

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
    [onVerify, onSuccess, onError]
  )

  const trySubmitCode = useCallback(() => {
    const pending = pendingCodeRef.current
    if (useWebAuthn || useBackupCode || isVerifying) {
      return
    }

    if (!pending || pending.length !== 6 || !DIGITS_ONLY_REGEX.test(pending)) {
      return
    }
    if (lastSubmittedCodeRef.current === pending) {
      return
    }

    pendingCodeRef.current = null
    verifyCode(pending, trustDevice).catch(() => {
      /* errors handled in verifyCode */
    })
  }, [isVerifying, trustDevice, useBackupCode, useWebAuthn, verifyCode])

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

  // Auto-trigger WebAuthn verification once when it's the user's preferred method.
  // Only triggers when server says preferred_method is webauthn, not when user manually switches
  useEffect(() => {
    // Don't auto-trigger until MFA status is loaded
    if (!mfaStatus) return
    // Don't auto-trigger if user doesn't have WebAuthn enrolled
    if (!hasWebAuthn) return

    if (
      autoTriggerWebAuthn &&
      useWebAuthn &&
      !useBackupCode &&
      !autoTriggeredRef.current &&
      !isVerifying &&
      !error &&
      mfaStatus.preferred_method === 'webauthn'
    ) {
      autoTriggeredRef.current = true
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
    useBackupCode,
    useWebAuthn,
  ])

  const view = resolveFormView(isMFAStatusLoading, useBackupCode, useWebAuthn)

  const renderProps: RenderProps = {
    showTrustDevice,
    trustDevice,
    setTrustDevice,
    isVerifying,
    handleVerifyWebAuthn: () => {
      handleVerifyWebAuthn().catch(() => {
        /* errors handled by onError */
      })
    },
    allowMethodSwitch,
    hasTOTP,
    hasWebAuthn,
    hasBackupCodes,
    useWebAuthn,
    setUseWebAuthn,
    setUseBackupCode: (value: boolean) => {
      setError(null)
      setUseBackupCode(value)
    },
    handleVerifyBackupCode: (backupCode: string) => {
      verifyCode(backupCode, false).catch(() => {
        /* errors handled in verifyCode */
      })
    },
    renderCancelButton,
    autoFocus,
    inputVersion,
    code,
    setError,
    setCode,
    pendingCodeRef,
    trySubmitCode,
  }

  return (
    <div className="space-y-6">
      <p
        className={`text-center text-muted-foreground text-sm ${isMFAStatusLoading ? 'invisible' : ''}`}
      >
        {describeMFAView(view, mode)}
      </p>
      {renderFormContent(view, renderProps)}
    </div>
  )
}
