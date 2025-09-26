import { Button } from '@/components/ui/button'
import { AuthResultCard } from '@/components/auth-result-card'

export function CLIAuthSuccessPage() {
  return (
    <AuthResultCard
      description="Your CLI login is approved. Return to the terminal window that prompted you and continue with your workflow."
      status="success"
      title="Authentication Complete"
    >
      <Button asChild className="w-full sm:w-auto">
        <a href="/.gateway/web/">Open Web UI</a>
      </Button>
    </AuthResultCard>
  )
}

export default CLIAuthSuccessPage
