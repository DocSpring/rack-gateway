import { RefreshCcw } from 'lucide-react'
import { AuthResultCard } from '@/components/auth-result-card'
import { Button } from '@/components/ui/button'

interface LoginError {
  title: string
  description: string
}

// Reason codes the gateway sends a failed web login here with (see webLoginError in
// internal/gateway/handlers/auth_web_errors.go). The code only selects a message and is never
// shown, so a crafted link can't put its own text on this page.
const LOGIN_ERRORS = new Map<string, LoginError>([
  [
    'access_denied',
    {
      title: 'Sign-in canceled',
      description: 'The sign-in was canceled or denied at the identity provider.',
    },
  ],
  [
    'identity_provider_error',
    {
      title: 'Unable to sign in',
      description: 'The identity provider reported an error. Please try again.',
    },
  ],
  [
    'missing_code',
    {
      title: 'Unable to sign in',
      description: "The identity provider didn't return a sign-in code. Please try again.",
    },
  ],
  [
    'state_mismatch',
    {
      title: 'Sign-in link mismatch',
      description:
        "This sign-in didn't start in this browser, or it took too long. Please sign in again.",
    },
  ],
  [
    'exchange_failed',
    {
      title: 'Unable to sign in',
      description: "The identity provider sign-in couldn't be completed. Please try again.",
    },
  ],
  [
    'unauthorized',
    {
      title: 'Account not authorized',
      description:
        'You are not authorized to access this application. Please contact your administrator.',
    },
  ],
  [
    'session_failed',
    {
      title: 'Unable to sign in',
      description: "The gateway couldn't start your session. Please try again.",
    },
  ],
  [
    'audit_unavailable',
    {
      title: 'Sign-in unavailable',
      description:
        "The gateway can't write to its audit log, so it refuses every action, including sign-ins. Please contact your administrator.",
    },
  ],
])

const UNKNOWN_ERROR: LoginError = {
  title: 'Unable to sign in',
  description: 'Something went wrong while completing your login. Please try again.',
}

function loginError(code: string | null): LoginError {
  return (code && LOGIN_ERRORS.get(code)) || UNKNOWN_ERROR
}

// The gateway sends the browser here when a web login fails.
export function LoginErrorPage() {
  const info = loginError(new URLSearchParams(window.location.search).get('error'))

  return (
    <AuthResultCard description={info.description} status="error" title={info.title}>
      <div className="flex justify-center">
        <Button asChild className="w-full sm:w-auto">
          <a href="/app/login">
            <RefreshCcw className="mr-2 h-4 w-4" /> Try again
          </a>
        </Button>
      </div>
    </AuthResultCard>
  )
}
