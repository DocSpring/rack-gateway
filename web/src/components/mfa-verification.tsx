import { useMutation, useQuery } from '@tanstack/react-query'
import { AlertCircle } from 'lucide-react'
import { useEffect, useState } from 'react'
import { LoadingSpinner } from '@/components/loading-spinner'
import { MFAInput } from '@/components/mfa-input'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { toast } from '@/components/ui/use-toast'
import {
  getMFAStatus,
  startWebAuthnAssertion,
  verifyCliMfa,
  verifyMFA,
  verifyWebAuthnAssertion,
} from '@/lib/api'
import { getErrorMessage } from '@/lib/error-utils'
import { prepareRequestOptions, serializeAssertionCredential } from '@/lib/webauthn-utils'

type MFAVerificationProps = {
  mode: 'web' | 'cli' | 'step-up'
  onSuccess: (result?: unknown) => void | Promise<void>
  onError?: (error: unknown) => void
  showTrustDevice?: boolean
  trustDeviceDefault?: boolean
  autoFocus?: boolean
  errorMessage?: string | null
  cliState?: string // Required for CLI mode
}

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Shared MFA component handles multiple flows (web/CLI, TOTP/WebAuthn).
export function MFAVerification({
  mode,
  onSuccess,
  onError,
  showTrustDevice = true,
  trustDeviceDefault = true,
  autoFocus = true,
  errorMessage = null,
  cliState,
}: MFAVerificationProps) {
  const [code, setCode] = useState('')
  const [trustDevice, setTrustDevice] = useState(trustDeviceDefault)
  const [error, setError] = useState<string | null>(errorMessage)
  const [useWebAuthn, setUseWebAuthn] = useState(false)

  // Fetch MFA status to know what methods are available
  const { data: mfaStatus } = useQuery({
    queryKey: ['mfa-status'],
    queryFn: getMFAStatus,
    retry: false,
    staleTime: 30_000,
  })

  const hasWebAuthn = (mfaStatus?.methods?.filter((m) => m.type === 'webauthn').length ?? 0) > 0
  const hasTOTP = (mfaStatus?.methods?.filter((m) => m.type === 'totp').length ?? 0) > 0

  // Default to WebAuthn if only WebAuthn is available
  useEffect(() => {
    if (mfaStatus && !hasTOTP && hasWebAuthn) {
      setUseWebAuthn(true)
    }
  }, [mfaStatus, hasTOTP, hasWebAuthn])

  // Auto-trigger WebAuthn when selected
  // biome-ignore lint/correctness/useExhaustiveDependencies: Only run when useWebAuthn becomes true
  useEffect(() => {
    if (useWebAuthn && !webAuthnMutation.isPending && mfaStatus) {
      handleWebAuthn()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [useWebAuthn])

  const totpMutation = useMutation({
    mutationFn: async () => {
      if (mode === 'cli') {
        if (!cliState) {
          throw new Error('CLI state is required for CLI mode')
        }
        return verifyCliMfa({ state: cliState, code: code.trim() })
      }
      await verifyMFA({ code: code.trim(), trust_device: trustDevice })
      return null
    },
    onSuccess: async (result) => {
      toast.success('MFA verification successful')
      await onSuccess(result)
    },
    onError: (err) => {
      const message = getErrorMessage(err, 'Verification failed')
      setError(message)
      onError?.(err)
    },
  })

  const webAuthnMutation = useMutation({
    mutationFn: async () => {
      const assertionStart = await startWebAuthnAssertion()
      if (!assertionStart.options) {
        throw new Error('No assertion options received from server')
      }

      const credentialRequestOptions = prepareRequestOptions(assertionStart.options)
      const credential = await navigator.credentials.get({
        publicKey: credentialRequestOptions,
      })

      if (!credential) {
        throw new Error('No credential received from authenticator')
      }

      const assertionResponse = serializeAssertionCredential(credential as PublicKeyCredential)

      if (mode === 'cli') {
        if (!cliState) {
          throw new Error('CLI state is required for CLI mode')
        }
        return verifyCliMfa({
          state: cliState,
          method: 'webauthn',
          session_data: assertionStart.session_data ?? '',
          assertion_response: JSON.stringify(assertionResponse),
        })
      }

      await verifyWebAuthnAssertion({
        session_data: assertionStart.session_data ?? '',
        assertion_response: JSON.stringify(assertionResponse),
        trust_device: trustDevice,
      })
      return null
    },
    onSuccess: async (result) => {
      toast.success('MFA verification successful')
      await onSuccess(result)
    },
    onError: (err) => {
      const message = getErrorMessage(err, 'Verification failed')
      setError(message)
      onError?.(err)
    },
  })

  const handleVerify = () => {
    if (!code.trim()) {
      setError('Enter a verification code to continue')
      return
    }
    setError(null)
    totpMutation.mutate()
  }

  const handleWebAuthn = () => {
    setError(null)
    webAuthnMutation.mutate()
  }

  const isVerifying = totpMutation.isPending || webAuthnMutation.isPending

  return (
    <div className="space-y-4">
      {error && (
        <Alert variant="destructive">
          <AlertCircle className="size-4" />
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {useWebAuthn ? (
        <>
          <div className="space-y-4">
            <p className="text-muted-foreground text-sm">
              Click the button below to authenticate with your security key or biometric device.
            </p>
            <Button className="w-full" disabled={isVerifying} onClick={handleWebAuthn}>
              {webAuthnMutation.isPending ? (
                <LoadingSpinner className="size-4" variant="white" />
              ) : (
                'Authenticate with Security Key'
              )}
            </Button>
          </div>
          {showTrustDevice && (
            <label className="flex items-center gap-2 text-sm">
              <input
                checked={trustDevice}
                onChange={(event) => setTrustDevice(event.target.checked)}
                type="checkbox"
              />
              Trust this browser for 30 days
            </label>
          )}
          {hasTOTP && hasWebAuthn && (
            <div className="border-t pt-4">
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
        </>
      ) : (
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault()
            handleVerify()
          }}
        >
          <div className="space-y-2">
            <Label htmlFor="mfa-code">Verification code</Label>
            <MFAInput
              autoFocus={autoFocus}
              id="mfa-code"
              maxLength={mode === 'cli' ? 12 : 6}
              onChange={(event) => {
                setError(null)
                setCode(event.target.value.trim())
              }}
              onComplete={() => handleVerify()}
              placeholder="123456"
              required
              value={code}
            />
          </div>
          {showTrustDevice && (
            <label className="flex items-center gap-2 text-sm">
              <input
                checked={trustDevice}
                onChange={(event) => setTrustDevice(event.target.checked)}
                type="checkbox"
              />
              Trust this browser for 30 days
            </label>
          )}
          {hasWebAuthn && (
            <div className="border-t pt-4">
              <Button
                className="w-full"
                onClick={() => setUseWebAuthn(true)}
                type="button"
                variant="outline"
              >
                Use security key instead
              </Button>
            </div>
          )}
          <Button className="w-full" disabled={isVerifying || code.length === 0} type="submit">
            {totpMutation.isPending ? 'Verifying…' : 'Verify'}
          </Button>
        </form>
      )}
    </div>
  )
}
