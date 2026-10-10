import { AuthResultCard } from '@/components/auth-result-card'
import { Button } from '@/components/ui/button'
import { WebRoute } from '@/lib/routes'

// The rack-gateway CLI sends the browser here once it has redeemed the login and saved the session.
export function CLIAuthSuccessPage() {
  return (
    <AuthResultCard
      description="Your CLI login is approved. Return to the terminal window that prompted you and continue with your workflow."
      status="success"
      title="Authentication Complete"
    >
      <Button asChild className="w-full sm:w-auto">
        <a href={WebRoute('/')}>Open Web UI</a>
      </Button>
    </AuthResultCard>
  )
}
