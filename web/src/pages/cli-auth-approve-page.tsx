import { useMutation } from '@tanstack/react-query'
import { isAxiosError } from 'axios'
import { AlertCircle } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { LoadingSpinner } from '@/components/loading-spinner'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { verifyCliMfa } from '@/lib/api'
import { WebRoute } from '@/lib/routes'

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

export function CLIAuthApprovePage() {
  const search = useMemo(() => new URLSearchParams(window.location.search), [])
  const state = extractParam(search, 'state')
  const presetError = mapQueryError(extractParam(search, 'error'))

  const [code, setCode] = useState('')
  const [error, setError] = useState<string | null>(presetError)

  useEffect(() => {
    if (!state) {
      setError(mapQueryError('missing_state'))
    }
  }, [state])

  const mutation = useMutation({
    mutationFn: verifyCliMfa,
    onSuccess: () => {
      window.location.assign(WebRoute('cli/auth/success'))
    },
    onError: (err: unknown) => {
      if (isAxiosError(err)) {
        const serverCode = err.response?.data?.error
        switch (serverCode) {
          case 'session_expired':
            setError(
              'This login session has expired. Return to your terminal and start the login again.'
            )
            return
          case 'invalid_code':
            setError('Invalid authentication code. Please try again.')
            return
          case 'session_incomplete':
            setError(
              'This login session is incomplete. Close the window and start the login again from your terminal.'
            )
            return
          case 'state_and_code_required':
            setError('State and code are both required to approve the login.')
            return
          case 'load_failure':
            setError('We could not load the login session. Try again from your terminal.')
            return
          case 'exchange_failed':
            setError(
              'Unable to complete authentication with the identity provider. Restart the login from your terminal.'
            )
            return
          case 'unauthorized':
            setError('You do not have access to this gateway.')
            return
          case 'service_unavailable':
            setError('Login approval is temporarily unavailable. Try again shortly.')
            return
          case 'persist_failure':
            setError('Failed to finalise the login approval. Please try again.')
            return
          default:
            if (typeof serverCode === 'string' && serverCode.trim() !== '') {
              setError(serverCode)
              return
            }
        }
      } else if (err instanceof Error) {
        setError(err.message)
        return
      }
      setError('Invalid authentication code. Please try again.')
    },
  })

  const handleVerify = () => {
    if (!state) {
      setError('Missing login session information. Close this window and try again from the CLI.')
      return
    }
    setError(null)
    mutation.mutate({ state, code })
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-background px-6 py-10">
      <Card className="w-full max-w-lg">
        <CardHeader className="space-y-3">
          <CardTitle>Approve CLI Login</CardTitle>
          <p className="text-muted-foreground text-sm">
            Enter the 6-digit code from your authenticator app to confirm this CLI login request.
          </p>
        </CardHeader>
        <CardContent className="space-y-6">
          {error ? (
            <Alert variant="destructive">
              <AlertCircle className="size-4" />
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}
          <div className="space-y-2">
            <Label htmlFor="mfa-code">Verification code</Label>
            <Input
              autoCapitalize="none"
              autoComplete="one-time-code"
              autoCorrect="off"
              autoFocus
              id="mfa-code"
              inputMode="numeric"
              maxLength={12}
              name="otp_entry"
              onChange={(event) => {
                setError(null)
                setCode(event.target.value.replace(/\s+/g, ''))
              }}
              pattern="[0-9]*"
              placeholder="123456"
              type="text"
              value={code}
            />
          </div>
        </CardContent>
        <CardFooter className="flex flex-col gap-3 sm:flex-row sm:justify-between sm:gap-4">
          <Button
            className="w-full sm:w-auto"
            disabled={!state || code.length < 6 || mutation.isPending}
            onClick={handleVerify}
          >
            {mutation.isPending ? <LoadingSpinner className="size-4" /> : 'Verify and Continue'}
          </Button>
          <Button
            className="w-full sm:w-auto"
            onClick={() => window.location.assign(WebRoute('/'))}
            type="button"
            variant="outline"
          >
            Cancel
          </Button>
        </CardFooter>
      </Card>
    </div>
  )
}

export default CLIAuthApprovePage
