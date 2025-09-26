import { AlertTriangle, RefreshCcw } from 'lucide-react'
import { useMemo } from 'react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

const REASON_MESSAGES: Record<string, { title: string; description: string }> = {
  'mfa-finalize': {
    title: 'Multi-factor verification failed',
    description:
      "We couldn't finish verifying your authenticator. Try signing in again and complete the MFA prompt.",
  },
}

export function LoginErrorPage() {
  const { reason, message } = useMemo(() => {
    if (typeof window === 'undefined') {
      return { reason: null as string | null, message: null as string | null }
    }
    const params = new URLSearchParams(window.location.search)
    return {
      reason: params.get('reason'),
      message: params.get('message'),
    }
  }, [])

  const info = (reason && REASON_MESSAGES[reason]) || {
    title: 'Unable to sign in',
    description: 'Something went wrong while completing your login. Please try again.',
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-background px-6 py-12">
      <Card className="w-full max-w-lg text-center">
        <CardHeader className="items-center gap-3">
          <div className="rounded-full bg-destructive/10 p-3 text-destructive">
            <AlertTriangle className="h-8 w-8" />
          </div>
          <CardTitle className="text-2xl">{info.title}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-6 px-6 text-left text-sm">
          <Alert variant="destructive">
            <AlertDescription>
              {message && message.trim().length > 0 ? message : info.description}
            </AlertDescription>
          </Alert>
          <div className="flex justify-center">
            <Button asChild className="w-full sm:w-auto">
              <a href="/.gateway/web/login">
                <RefreshCcw className="mr-2 h-4 w-4" /> Try again
              </a>
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

export default LoginErrorPage
