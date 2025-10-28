import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

type AuditStatsCardsProps = {
	totalCount: number
	successfulEvents: number
	totalEvents: number
	failedEvents: number
	deniedEvents: number
	averageResponseTimeMs: number
}

export function AuditStatsCards({
	totalCount,
	successfulEvents,
	totalEvents,
	failedEvents,
	deniedEvents,
	averageResponseTimeMs,
}: AuditStatsCardsProps) {
	const successRate =
		totalEvents > 0 ? Math.round((successfulEvents / totalEvents) * 100) : 0
	const failedAndDenied = failedEvents + deniedEvents

	return (
		<div className="mb-6 grid gap-6 md:grid-cols-4">
			<Card>
				<CardHeader className="pb-2">
					<CardTitle className="font-medium text-muted-foreground text-sm">
						Total Logs
					</CardTitle>
				</CardHeader>
				<CardContent>
					<div className="font-bold text-2xl">{totalCount}</div>
				</CardContent>
			</Card>
			<Card>
				<CardHeader className="pb-2">
					<CardTitle className="font-medium text-muted-foreground text-sm">
						Success Rate
					</CardTitle>
				</CardHeader>
				<CardContent>
					<div className="font-bold text-2xl text-green-600">{successRate}%</div>
				</CardContent>
			</Card>
			<Card>
				<CardHeader className="pb-2">
					<CardTitle className="font-medium text-muted-foreground text-sm">
						Failed/Denied
					</CardTitle>
				</CardHeader>
				<CardContent>
					<div className="font-bold text-2xl text-red-600">{failedAndDenied}</div>
				</CardContent>
			</Card>
			<Card>
				<CardHeader className="pb-2">
					<CardTitle className="font-medium text-muted-foreground text-sm">
						Avg Response Time
					</CardTitle>
				</CardHeader>
				<CardContent>
					<div className="font-bold text-2xl">{averageResponseTimeMs}ms</div>
				</CardContent>
			</Card>
		</div>
	)
}
