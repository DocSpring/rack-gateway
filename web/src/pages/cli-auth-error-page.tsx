import { AuthResultCard } from '@/components/auth-result-card'

type CLILoginError = { title: string; description: string }

// Error codes a failed CLI login ends with: the gateway's codes (delivered to the CLI's loopback
// listener) and the CLI's own. The code only selects a message and is never shown, so a crafted link
// can't put its own text on this page.
const CLI_LOGIN_ERRORS = new Map<string, CLILoginError>([
  ['canceled', { title: 'Login canceled', description: 'This CLI login was canceled.' }],
  [
    'access_denied',
    {
      title: 'Sign-in canceled',
      description: 'The sign-in was canceled or denied at the identity provider.',
    },
  ],
  [
    'unauthorized',
    {
      title: 'Account not authorized',
      description:
        "Your account isn't authorized for this gateway. Ask an administrator for access.",
    },
  ],
  [
    'exchange_failed',
    {
      title: 'Sign-in failed',
      description: "The identity provider sign-in couldn't be completed.",
    },
  ],
  [
    'identity_provider_error',
    { title: 'Sign-in failed', description: 'The identity provider reported an error.' },
  ],
  [
    'session_incomplete',
    { title: 'Login incomplete', description: 'The login session was incomplete.' },
  ],
  [
    'session_failed',
    { title: 'Login failed', description: "The gateway couldn't start a browser session." },
  ],
  [
    'persist_failure',
    { title: 'Login failed', description: "The gateway couldn't save the login." },
  ],
  ['load_failure', { title: 'Login failed', description: "The gateway couldn't load the login." }],
  [
    'state_mismatch',
    {
      title: 'Login link mismatch',
      description: "This link doesn't belong to the login waiting in your terminal.",
    },
  ],
  [
    'missing_code',
    { title: 'Login failed', description: "The gateway didn't return a login code." },
  ],
  [
    'cli_incomplete',
    {
      title: 'Login not completed',
      description: "Your terminal couldn't finish the login. Check it for details.",
    },
  ],
])

const UNKNOWN_ERROR: CLILoginError = {
  title: 'CLI login failed',
  description: 'Something went wrong while completing your CLI login.',
}

function cliLoginError(code: string | null): CLILoginError {
  return (code && CLI_LOGIN_ERRORS.get(code)) || UNKNOWN_ERROR
}

// The rack-gateway CLI sends the browser here when a login fails.
export function CLIAuthErrorPage() {
  const info = cliLoginError(new URLSearchParams(window.location.search).get('error'))

  return (
    <AuthResultCard description={info.description} status="error" title={info.title}>
      <p className="text-muted-foreground text-sm">
        Return to your terminal and run
        <code className="mx-1 rounded bg-muted px-1 py-0.5">rack-gateway login</code>
        to try again.
      </p>
    </AuthResultCard>
  )
}
