import { RefreshCcw } from 'lucide-react';
import { useMemo } from 'react';
import { AuthResultCard } from '@/components/auth-result-card';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';

const REASON_MESSAGES: Record<string, { title: string; description: string }> =
  {
    'mfa-finalize': {
      title: 'Multi-factor verification failed',
      description:
        "We couldn't finish verifying your authenticator. Try signing in again and complete the MFA prompt.",
    },
  };

export function LoginErrorPage() {
  const { reason, message } = useMemo(() => {
    if (typeof window === 'undefined') {
      return { reason: null as string | null, message: null as string | null };
    }
    const params = new URLSearchParams(window.location.search);
    return {
      reason: params.get('reason'),
      message: params.get('message'),
    };
  }, []);

  const info = (reason && REASON_MESSAGES[reason]) || {
    title: 'Unable to sign in',
    description:
      'Something went wrong while completing your login. Please try again.',
  };

  return (
    <AuthResultCard
      contentClassName="w-full space-y-6 text-left text-sm"
      description={info.description}
      status="error"
      title={info.title}
    >
      <Alert className="w-full" variant="destructive">
        <AlertDescription>
          {message && message.trim().length > 0 ? message : info.description}
        </AlertDescription>
      </Alert>
      <div className="flex justify-center">
        <Button asChild className="w-full sm:w-auto">
          <a href="/app/login">
            <RefreshCcw className="mr-2 h-4 w-4" /> Try again
          </a>
        </Button>
      </div>
    </AuthResultCard>
  );
}

export default LoginErrorPage;
