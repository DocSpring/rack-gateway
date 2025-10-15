import { useMutation } from '@tanstack/react-query'
import { isAxiosError } from 'axios'
import { AlertCircle } from 'lucide-react'
import { useMemo, useState } from 'react'
import { MFAVerificationForm } from '@/components/mfa-verification-form'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { verifyCliMfa, verifyMFA, verifyWebAuthnAssertion } from '@/lib/api'
import { authService } from '@/lib/auth'
import { normalizeRedirectPath } from '@/lib/navigation'
import { resolveWebRedirect, WebRoute } from '@/lib/routes'

type ChallengeMode = 'cli' | 'web'

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
  const target = result?.redirect?.trim()
  return target && target !== '' ? target : WebRoute('cli/auth/success')
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

export function MFAChallengePage() {
  const search = useMemo(() => new URLSearchParams(window.location.search), [])
  const state = extractParam(search, 'state')
  const channel = extractParam(search, 'channel') ?? extractParam(search, 'flow')
  const redirectParam = extractParam(search, 'redirect')
  const presetError = mapQueryError(extractParam(search, 'error'))

  const mode = resolveMode(channel, state)
  const redirectTarget = useMemo(() => normalizeRedirectPath(redirectParam), [redirectParam])

  const [error, setError] = useState<string | null>(presetError)
  const [hasRedirected, setHasRedirected] = useState(false)

  const mutation = useMutation<
    CLICompletion | null,
    unknown,
    { code: string; trust_device: boolean }
  >({
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
        const target = result?.redirect?.trim()
        const destination = target && target !== '' ? target : WebRoute('cli/auth/success')
        window.location.assign(destination)
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

export default MFAChallengePage
