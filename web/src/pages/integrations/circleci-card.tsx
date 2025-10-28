import { CheckCircle2, Circle } from 'lucide-react'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'

import type { CircleCISettings } from '@/pages/integrations/types'

type CircleCiCardProps = {
	settings?: CircleCISettings | null
}

export function CircleCiCard({ settings }: CircleCiCardProps) {
	const enabled = Boolean(settings?.api_token?.trim() && settings?.org_slug?.trim())

	return (
		<Card>
			<CardHeader>
				<div className="flex items-start justify-between">
					<div>
						<CardTitle className="flex items-center gap-2">
							<svg className="size-6" fill="currentColor" viewBox="0 0 103.8 105.2">
								<title>CircleCI logo</title>
								<path d="m 38.6,52.6 c 0,-6.9 5.6,-12.5 12.5,-12.5 6.9,0 12.5,5.6 12.5,12.5 0,6.9 -5.6,12.5 -12.5,12.5 C 44.2,65.2 38.6,59.5 38.6,52.6 Z M 51.1,0 C 26.5,0 5.9,16.8 0.1,39.6 0.1,39.8 0,39.9 0,40.1 c 0,1.4 1.1,2.5 2.5,2.5 l 21.2,0 c 1,0 1.9,-0.6 2.3,-1.5 l 0,0 C 30.4,31.6 39.9,25 51.1,25 66.3,25 78.7,37.4 78.7,52.6 78.7,67.8 66.3,80.2 51.1,80.2 40,80.2 30.4,73.6 26,64.1 l 0,0 c -0.4,-0.9 -1.3,-1.5 -2.3,-1.5 l -21.2,0 c -1.4,0 -2.5,1.1 -2.5,2.5 0,0.2 0,0.3 0.1,0.5 5.8,22.8 26.4,39.6 51,39.6 29.1,0 52.7,-23.6 52.7,-52.7 C 103.8,23.5 80.2,0 51.1,0 Z" />
							</svg>
							CircleCI
						</CardTitle>
						<CardDescription>Auto-approve CircleCI jobs after deploy approval</CardDescription>
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
								CircleCI integration is enabled. The gateway will automatically approve CircleCI
								jobs when deploy approvals are granted.
							</AlertDescription>
						</Alert>
						<div className="rounded border bg-muted p-4">
							<div className="space-y-2">
								<div className="flex justify-between text-sm">
									<span className="font-medium">Organization:</span>
									<code className="rounded bg-background px-2 py-1">
										{settings?.org_slug}
									</code>
								</div>
								<div className="flex justify-between text-sm">
									<span className="font-medium">API Token:</span>
									<span className="text-muted-foreground">Configured</span>
								</div>
							</div>
						</div>
						<p className="text-muted-foreground text-xs">
							API token set via{' '}
							<code className="ml-1 rounded bg-muted px-1 py-0.5">CIRCLECI_TOKEN</code> env var.
						</p>
						<p className="text-muted-foreground text-xs">
							Per-app organization slugs and job names are configured in app settings.
						</p>
					</div>
				) : (
					<div className="flex flex-col gap-4">
						<p className="text-muted-foreground text-sm">
							Set the CircleCI API token environment variable to enable:
						</p>
						<div className="w-full space-y-2 rounded border bg-muted p-4 font-mono text-sm">
							<div>CIRCLECI_TOKEN=your-api-token</div>
						</div>
						<p className="text-muted-foreground text-xs">
							Per-app organization slugs are configured in app settings.
						</p>
					</div>
				)}
			</CardContent>
		</Card>
	)
}
