import { AuthResultCard } from '@/components/auth-result-card'
import { Button } from '@/components/ui/button'

export function CLIAuthSuccessPage() {
  return (
    <AuthResultCard
      description="Your CLI login is approved. Return to the terminal window that prompted you and continue with your workflow."
      status="success"
      title="Authentication Complete"
    >
      <Button asChild className="w-full sm:w-auto">
        <a href="/app/">Open Web UI</a>
      </Button>
    </AuthResultCard>
  )
}

export default CLIAuthSuccessPage
