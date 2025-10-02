import { useMutation, useQuery } from '@tanstack/react-query'
import { isAxiosError } from 'axios'
import { AlertCircle } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { LoadingSpinner } from '@/components/loading-spinner'
import { MFAInput } from '@/components/mfa-input'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import {
  getMFAStatus,
  startWebAuthnAssertion,
  verifyCliMfa,
  verifyMFA,
  verifyWebAuthnAssertion,
} from '@/lib/api'
import { authService } from '@/lib/auth'
import { normalizeRedirectPath } from '@/lib/navigation'
import { DEFAULT_WEB_ROUTE, WebRoute } from '@/lib/routes'
import {
  getCredential,
  prepareRequestOptions,
  serializeAssertionCredential,
} from '@/lib/webauthn-utils'

type ChallengeMode = 'cli' | 'web'

type MutationPayload = {
  code: string
  trustDevice: boolean
}

type CLICompletion = {
  redirect: string
}

const CLI_ERROR_MESSAGES: Record<string, string> = {
  session_expired:
    'This login session has expired. Return to your terminal and start the login again.',
  invalid_code: 'Invalid authentication code. Please try again.',
  session_incomplete:
    'This login session is incomplete. Close this window and start the login again from your terminal.',
  state_and_code_required: 'State and code are both required to approve the login.',
  load_failure: 'We could not load the login session. Try again from your terminal.',
  exchange_failed:
    'Unable to complete authentication with the identity provider. Restart the login from your terminal.',
  unauthorized: 'You do not have access to this gateway.',
  service_unavailable: 'Login approval is temporarily unavailable. Try again shortly.',
  persist_failure: 'Failed to finalise the login approval. Please try again.',
}

const WEB_ERROR_MESSAGES: Record<string, string> = {
  'code required': 'Enter the code from your authenticator app.',
  'invalid code': 'Invalid authentication code. Please try again.',
  'mfa requires user session': 'Your login session expired. Please sign in again.',
  'mfa service unavailable':
    'Multi-factor authentication is temporarily unavailable. Try again shortly.',
}

const FALLBACK_ERROR = 'Invalid authentication code. Please try again.'

function extractParam(search: URLSearchParams, key: string): string | null {
  const value = search.get(key)
  if (!value) return null
  const trimmed = value.trim()
  return trimmed === '' ? null : trimmed
}

function mapQueryError(code: string | null): string | null {
  switch (code) {
    case 'missing_state':
      return 'This login session is missing required information. Close this window and rerun the login command from your terminal.'
    case 'service_unavailable':
      return 'Login approval is temporarily unavailable. Try again shortly.'
    case 'load_failure':
      return 'We could not load the login session. Try again from your terminal.'
    case 'expired':
      return 'This login session has expired. Return to your terminal and start the login again.'
    default:
      return code
  }
}

function mapServerError(mode: ChallengeMode, error: unknown): string {
  if (isAxiosError(error)) {
    const raw = error.response?.data?.error
    if (typeof raw === 'string') {
      const trimmed = raw.trim()
      if (trimmed !== '') {
        const lookup = mode === 'cli' ? CLI_ERROR_MESSAGES : WEB_ERROR_MESSAGES
        return lookup[trimmed] ?? trimmed
      }
    }
  } else if (error instanceof Error) {
    const message = error.message.trim()
    if (message !== '') {
      return message
    }
  }
  return FALLBACK_ERROR
}

function resolveMode(channelParam: string | null, state: string | null): ChallengeMode {
  if (channelParam === 'cli') {
    return 'cli'
  }
  if (channelParam === 'web') {
    return 'web'
  }
  return state ? 'cli' : 'web'
}

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: MFA management page coordinates multiple flows.
export function MFAChallengePage() {
  const search = useMemo(() => new URLSearchParams(window.location.search), [])
  const state = extractParam(search, 'state')
  const channel = extractParam(search, 'channel') ?? extractParam(search, 'flow')
  const redirectParam = extractParam(search, 'redirect')
  const presetError = mapQueryError(extractParam(search, 'error'))

  const mode = resolveMode(channel, state)

  const redirectTarget = useMemo(() => normalizeRedirectPath(redirectParam), [redirectParam])
  const [code, setCode] = useState('')
  const [trustDevice, setTrustDevice] = useState(true)
  const [error, setError] = useState<string | null>(presetError)
  const [useWebAuthn, setUseWebAuthn] = useState(false)

  // Use ref to always get latest trustDevice value, avoiding closure capture issues
  const trustDeviceRef = useRef(trustDevice)
  useEffect(() => {
    trustDeviceRef.current = trustDevice
  }, [trustDevice])

  // Try to fetch MFA status to determine available methods and preferred method
  // Enable for both web and CLI modes
  const { data: mfaStatus } = useQuery({
    queryKey: ['mfaStatus'],
    queryFn: getMFAStatus,
    retry: false,
    staleTime: 30_000,
  })

  // Determine which methods are available
  const hasWebAuthn = mfaStatus?.methods?.some((m) => m.type === 'webauthn') ?? false
  const hasTOTP = mfaStatus?.methods?.some((m) => m.type === 'totp') ?? false

  // Set initial method based on preferred method or available methods
  useEffect(() => {
    if (!mfaStatus) return

    // Use preferred method if set
    if (mfaStatus.preferred_method === 'webauthn' && hasWebAuthn) {
      setUseWebAuthn(true)
      return
    }
    if (mfaStatus.preferred_method === 'totp' && hasTOTP) {
      setUseWebAuthn(false)
      return
    }

    // Fallback: If user only has WebAuthn, default to that
    if (hasWebAuthn && !hasTOTP) {
      setUseWebAuthn(true)
      return
    }

    // Otherwise default to TOTP (most common)
    setUseWebAuthn(false)
  }, [mfaStatus, hasWebAuthn, hasTOTP])

  useEffect(() => {
    if (mode === 'cli' && !state) {
      setError(mapQueryError('missing_state'))
    }
  }, [mode, state])

  const mutation = useMutation<CLICompletion | null, unknown, MutationPayload>({
    mutationFn: async ({ code: submittedCode, trustDevice: trustDevicePreference }) => {
      if (mode === 'cli') {
        if (!state) {
          throw new Error(
            'Missing login session information. Close this window and try again from the CLI.'
          )
        }
        return verifyCliMfa({ state, code: submittedCode })
      }

      await verifyMFA({ code: submittedCode, trust_device: trustDevicePreference })
      return null
    },
    onSuccess: (result) => {
      if (mode === 'cli') {
        const target = result?.redirect?.trim()
        const destination = target && target !== '' ? target : WebRoute('cli/auth/success')
        window.location.assign(destination)
        return
      }

      const defaultDestination = redirectTarget
        ? WebRoute(redirectTarget.startsWith('/') ? redirectTarget.slice(1) : redirectTarget)
        : DEFAULT_WEB_ROUTE

      window.location.assign(defaultDestination)
    },
    onError: (err) => {
      setError(mapServerError(mode, err))
    },
  })

  const webAuthnMutation = useMutation({
    mutationFn: async () => {
      // Start the WebAuthn assertion flow
      const assertionStart = await startWebAuthnAssertion()

      if (!assertionStart.options) {
        throw new Error('No assertion options received from server')
      }

      // Convert server options to browser-compatible format
      const credentialRequestOptions = prepareRequestOptions(assertionStart.options)

      // Call the browser WebAuthn API to get an assertion
      const credential = await getCredential({
        publicKey: credentialRequestOptions,
      })

      if (!credential) {
        throw new Error('No credential received from authenticator')
      }

      // Serialize the assertion response for the backend
      const assertionResponse = serializeAssertionCredential(credential as PublicKeyCredential)

      // Read trustDevice from ref AFTER WebAuthn completes
      // The ref ensures we get the current value, not a stale closure capture
      // This way the user's final choice is used, even if they changed the checkbox
      // while the browser WebAuthn prompt was showing
      await verifyWebAuthnAssertion({
        session_data: assertionStart.session_data ?? '',
        assertion_response: JSON.stringify(assertionResponse),
        trust_device: trustDeviceRef.current,
      })

      return null
    },
    onSuccess: () => {
      const defaultDestination = redirectTarget
        ? WebRoute(redirectTarget.startsWith('/') ? redirectTarget.slice(1) : redirectTarget)
        : DEFAULT_WEB_ROUTE

      window.location.assign(defaultDestination)
    },
    onError: (err) => {
      setError(mapServerError(mode, err))
    },
  })

  const handleVerify = () => {
    if (code.trim().length < 6) {
      setError('Enter the code from your authenticator app.')
      return
    }
    if (mode === 'cli' && !state) {
      setError('Missing login session information. Close this window and try again from the CLI.')
      return
    }
    setError(null)
    mutation.mutate({ code: code.replace(/\s+/g, ''), trustDevice })
  }

  const handleWebAuthn = () => {
    setError(null)
    webAuthnMutation.mutate()
  }

  // Auto-trigger WebAuthn for web mode when it's the selected/only method
  useEffect(() => {
    if (
      mode === 'web' &&
      useWebAuthn &&
      !webAuthnMutation.isPending &&
      !webAuthnMutation.isSuccess &&
      !error
    ) {
      handleWebAuthn()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [useWebAuthn, mode])

  const handleLogout = () => {
    authService.logout()
  }

  const handleCancelCli = () => {
    if (window.opener) {
      window.close()
      return
    }
    window.location.assign(WebRoute('login'))
  }

  const disableVerify = code.trim().length < 6 || mutation.isPending || (mode === 'cli' && !state)
  const disableWebAuthn = webAuthnMutation.isPending || mutation.isPending

  const title = mode === 'cli' ? 'Approve CLI Login' : 'Multi-Factor Authentication Required'

  let description: string
  if (mode === 'cli') {
    description =
      'Enter the 6-digit code from your authenticator app to approve this CLI login request.'
  } else if (useWebAuthn) {
    description = 'Use your security key, Touch ID, or Windows Hello to sign in.'
  } else {
    description = 'Enter the 6-digit code from your authenticator app to finish signing in.'
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-background px-6 py-10">
      <Card className="w-full max-w-lg">
        <CardHeader className="space-y-3">
          <CardTitle>{title}</CardTitle>
          <p className="text-muted-foreground text-sm">{description}</p>
        </CardHeader>
        <CardContent className="space-y-6">
          {error ? (
            <Alert variant="destructive">
              <AlertCircle className="size-4" />
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}
          {useWebAuthn ? (
            <>
              <div className="space-y-4">
                <p className="text-muted-foreground text-sm">
                  Click the button below to authenticate with your security key or biometric device.
                </p>
                <Button className="w-full" disabled={disableWebAuthn} onClick={handleWebAuthn}>
                  {webAuthnMutation.isPending ? (
                    <LoadingSpinner className="size-4" variant="white" />
                  ) : (
                    'Authenticate with Security Key'
                  )}
                </Button>
              </div>
              <div className="flex gap-3 align-center text-sm">
                <input
                  checked={trustDevice}
                  className="h-4 w-4 rounded border border-input"
                  id="trust-device"
                  onChange={(event) => setTrustDevice(event.target.checked)}
                  type="checkbox"
                />
                <label className="leading-tight" htmlFor="trust-device">
                  Trust this device for 30 days
                </label>
              </div>
              {hasTOTP && (
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
            <>
              <div className="space-y-2">
                <Label htmlFor="mfa-code">Verification code</Label>
                <MFAInput
                  autoFocus
                  id="mfa-code"
                  maxLength={12}
                  onChange={(event) => {
                    setError(null)
                    setCode(event.target.value)
                  }}
                  onComplete={() => {
                    handleVerify()
                  }}
                  placeholder="123456"
                  value={code}
                />
              </div>
              {mode === 'web' ? (
                <>
                  <div className="flex items-center gap-3 text-sm">
                    <input
                      checked={trustDevice}
                      className="h-4 w-4 rounded border border-input"
                      id="trust-device"
                      onChange={(event) => setTrustDevice(event.target.checked)}
                      type="checkbox"
                    />
                    <label className="leading-tight" htmlFor="trust-device">
                      Trust this device for 30 days
                    </label>
                  </div>
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
                </>
              ) : null}
            </>
          )}
        </CardContent>
        <CardFooter className="flex flex-col gap-3 sm:flex-row sm:justify-between sm:gap-4">
          {!useWebAuthn || mode === 'cli' ? (
            <Button className="w-full sm:w-auto" disabled={disableVerify} onClick={handleVerify}>
              {mutation.isPending ? (
                <LoadingSpinner className="size-4" variant="white" />
              ) : (
                'Verify and Continue'
              )}
            </Button>
          ) : null}
          {mode === 'web' ? (
            <Button
              className="w-full sm:w-auto"
              onClick={handleLogout}
              type="button"
              variant="outline"
            >
              Logout
            </Button>
          ) : (
            <Button
              className="w-full sm:w-auto"
              onClick={handleCancelCli}
              type="button"
              variant="outline"
            >
              Cancel Login
            </Button>
          )}
        </CardFooter>
      </Card>
    </div>
  )
}

export default MFAChallengePage
