import { CheckCircle2, Circle } from 'lucide-react'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'

type GitHubCardProps = {
  enabled: boolean
}

export function GitHubCard({ enabled }: GitHubCardProps) {
  return (
    <Card>
      <CardHeader>
        <div className="flex items-start justify-between">
          <div>
            <CardTitle className="flex items-center gap-2">
              <svg className="size-6" fill="currentColor" viewBox="0 0 24 24">
                <title>GitHub logo</title>
                <path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z" />
              </svg>
              GitHub
            </CardTitle>
            <CardDescription>Verify commits and PRs for deployments</CardDescription>
          </div>
          <div className="flex items-center gap-2">
            {enabled ? (
              <CheckCircle2 className="size-5 text-green-600" />
            ) : (
              <Circle className="size-5 text-muted-foreground" />
            )}
          </div>
        </div>
      </CardHeader>
      <CardContent>
        {enabled ? (
          <div className="space-y-3">
            <Alert>
              <CheckCircle2 className="size-4" />
              <AlertDescription>
                GitHub integration is enabled. The gateway can verify git commits and PRs for deploy
                approval requests.
              </AlertDescription>
            </Alert>
            <p className="text-muted-foreground text-sm">
              API token set via{' '}
              <code className="ml-1 rounded bg-muted px-1 py-0.5">GITHUB_TOKEN</code> env var.
            </p>
            <p className="text-muted-foreground text-sm">
              Per-app repositories are configured in app settings.
            </p>
          </div>
        ) : (
          <div className="flex flex-col gap-4">
            <p className="text-muted-foreground text-sm">
              Set the GitHub API token environment variable to enable:
            </p>
            <div className="w-full space-y-2 rounded border bg-muted p-4 font-mono text-sm">
              <div>GITHUB_TOKEN=your-personal-access-token</div>
            </div>
            <p className="text-muted-foreground text-xs">
              Per-app repositories are configured in app settings.
            </p>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
