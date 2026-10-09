import { isAxiosError } from 'axios'
import { AlertCircle } from 'lucide-react'
import { useMemo, useState } from 'react'
import { MFAVerificationForm } from '@/components/mfa-verification-form'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { useMutation } from '@/hooks/use-mutation'
import { verifyCliMfa, verifyMFA, verifyWebAuthnAssertion } from '@/lib/api'
import { authService } from '@/lib/auth'
import { normalizeRedirectPath } from '@/lib/navigation'
import { resolveWebRedirect, WebRoute } from '@/lib/routes'

type ChallengeMode = 'cli' | 'web'

type CLICompletion = {
  redirect: string
}

const EXPIRED_MESSAGE =
  'This login session has expired. Return to your terminal and start the login again.'
const BROWSER_MISMATCH_MESSAGE =
  'This login was started in a different browser. Finish it in the browser that opened from your terminal, or start the login again.'
const MISSING_NEXT_STEP_MESSAGE =
  'The gateway did not return the next login step. Return to your terminal and start the login again.'

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
  browser_mismatch: BROWSER_MISMATCH_MESSAGE,
  expired: EXPIRED_MESSAGE,
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
      return EXPIRED_MESSAGE
    case 'browser_mismatch':
      return BROWSER_MISMATCH_MESSAGE
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

async function handleWebAuthnCLI(
  state: string | null,
  sessionData: string,
  assertionResponse: string
): Promise<string> {
  if (!state) {
    throw new Error(
      'Missing login session information. Close this window and try again from the CLI.'
    )
  }
  const result = await verifyCliMfa({
    state,
    method: 'webauthn',
    session_data: sessionData,
    assertion_response: assertionResponse,
  })
  return cliNextStep(result)
}

// cliNextStep returns the gateway URL that hands the browser back to the waiting CLI.
function cliNextStep(result: CLICompletion | null | undefined): string {
  const target = result?.redirect?.trim()
  if (!target) {
    throw new Error(MISSING_NEXT_STEP_MESSAGE)
  }
  return target
}

async function handleWebAuthnWeb(
  sessionData: string,
  assertionResponse: string,
  trustDevice: boolean,
  redirectTarget: string | null
): Promise<string> {
  await verifyWebAuthnAssertion({
    session_data: sessionData,
    assertion_response: assertionResponse,
    trust_device: trustDevice,
  })
  return resolveWebRedirect(redirectTarget)
}

// CLIInitiator shows where the CLI login was started so the user can spot a login they did not start.
function CLIInitiator({ device, ipAddress }: { device: string | null; ipAddress: string | null }) {
  if (!(device || ipAddress)) {
    return null
  }
  const parts = [device, ipAddress ? `IP ${ipAddress}` : null].filter(Boolean).join(', ')
  return (
    <p className="text-center text-muted-foreground text-sm" data-testid="cli-login-initiator">
      Login started from {parts}. If you did not just run <code>rack-gateway login</code>, cancel
      this login.
    </p>
  )
}

export function MFAChallengePage() {
  const search = useMemo(() => new URLSearchParams(window.location.search), [])
  const state = extractParam(search, 'state')
  const channel = extractParam(search, 'channel') ?? extractParam(search, 'flow')
  const redirectParam = extractParam(search, 'redirect')
  const presetError = mapQueryError(extractParam(search, 'error'))
  const initiatorDevice = extractParam(search, 'device')
  const initiatorIP = extractParam(search, 'ip')

  const mode = resolveMode(channel, state)
  const redirectTarget = useMemo(() => normalizeRedirectPath(redirectParam), [redirectParam])

  const [error, setError] = useState<string | null>(presetError)
  const [hasRedirected, setHasRedirected] = useState(false)

  const mutation = useMutation<
    CLICompletion | null,
    unknown,
    { code: string; trust_device: boolean }
  >({
    showToastError: false, // Errors are displayed in the Alert above, no need for toast
    mutationFn: async ({ code, trust_device }) => {
      if (mode === 'cli') {
        if (!state) {
          throw new Error(
            'Missing login session information. Close this window and try again from the CLI.'
          )
        }
        return verifyCliMfa({ state, code })
      }

      await verifyMFA({ code, trust_device })
      return null
    },
    onSuccess: (result) => {
      if (mode === 'cli') {
        try {
          window.location.assign(cliNextStep(result))
        } catch (err) {
          setError(mapServerError(mode, err))
        }
        return
      }

      const destination = resolveWebRedirect(redirectTarget)
      window.location.assign(destination)
    },
    onError: (err) => {
      setError(mapServerError(mode, err))
    },
  })

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

  const title = mode === 'cli' ? 'Approve CLI Login' : 'Multi-Factor Authentication Required'

  return (
    <div className="flex min-h-screen items-center justify-center bg-background px-6 py-10">
      <Card className="w-full max-w-xl">
        <CardHeader className="space-y-3 text-center">
          <CardTitle className="text-center">{title}</CardTitle>
          {mode === 'cli' ? (
            <CLIInitiator device={initiatorDevice} ipAddress={initiatorIP} />
          ) : null}
        </CardHeader>
        <CardContent className="space-y-6">
          {error ? (
            <Alert variant="destructive">
              <AlertCircle className="size-4" />
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}

          <MFAVerificationForm
            autoTriggerWebAuthn={mode === 'web'}
            mode={mode}
            onError={(err) => setError(mapServerError(mode, err))}
            onMFAStatusLoaded={(mfaStatus) => {
              // If user has no MFA methods enrolled, redirect to account security
              if (
                !hasRedirected &&
                mfaStatus &&
                (!mfaStatus.methods || mfaStatus.methods.length === 0)
              ) {
                setHasRedirected(true)
                const params = new URLSearchParams()
                params.set('enrollment', 'required')
                if (mode === 'cli' && state) {
                  params.set('channel', 'cli')
                  params.set('state', state)
                }
                window.location.assign(`${WebRoute('account/security')}?${params.toString()}`)
              }
            }}
            onVerify={async (params) => {
              if (params.method === 'totp') {
                await mutation.mutateAsync({
                  code: params.code,
                  trust_device: params.trust_device,
                })
                return
              }

              // WebAuthn
              const destination =
                mode === 'cli'
                  ? await handleWebAuthnCLI(state, params.session_data, params.assertion_response)
                  : await handleWebAuthnWeb(
                      params.session_data,
                      params.assertion_response,
                      params.trust_device,
                      redirectTarget
                    )
              window.location.assign(destination)
            }}
            showTrustDevice={mode === 'web'}
          />
        </CardContent>
        <CardFooter className="flex flex-col gap-3 sm:flex-row sm:justify-end sm:gap-4">
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
